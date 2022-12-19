package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
	"unicode"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	channelsJSONPath = "channels.json"
)

var (
	errNotInChannel = errors.New("not in channel")

	days30Ago = time.Now().AddDate(0, 0, -30)
)

func main() {
	slackBotToken := os.Getenv("SLACK_BOT_TOKEN")

	client := slack.New(slackBotToken)
	if _, err := client.AuthTest(); err != nil {
		log.Fatal(err)
	}

	// 1. 이모지를 불러오고 로컬에 저장한다. 저장된 이모지가 있다면 해당 파일을 불러옴
	if err := saveEmojis(client); err != nil {
		log.Fatal(err)
	}

	// 2. 조사할 채널을 불러온다. 저장된 채널 목록이 있다면 해당 파일을 불러옴
	channels, err := loadChannels(client)
	if err != nil {
		log.Fatal(err)
	}

	// 3. 채널을 돌면서 최근 n일 메시지를 불러와 저장함
	if err := run(client, channels); err != nil {
		log.Fatal(err)
	}
}

func saveEmojis(client *slack.Client) error {
	emojis, err := client.GetEmoji()
	if err != nil {
		return errors.Wrap(err, "GetEmoji")
	}
	return saveJSON("emojis.json", normalizeEmojis(emojis))
}

// 이모지를 만들 때 윈도우와 맥의 동작이 다른걸로 추정
// 어쨌든 유니코드를 정규화해서 자모분리가 되지 않도록 수정해줌
func normalizeEmojis(emojis map[string]string) map[string]string {
	m := make(map[string]string, len(emojis))
	for k, v := range emojis {
		m[normalize(k)] = v
	}
	return m
}

func normalize(s string) string {
	t := transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, err := transform.String(t, s)
	if err != nil {
		return s
	}

	return result
}

func saveJSON(name string, data interface{}) error {
	bb, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(name, bb, 0644); err != nil {
		return err
	}
	return nil
}

// 아카이브된 채널을 제외하고 모든 퍼블릭 채널을 불러온다
func listChannels(client *slack.Client) ([]slack.Channel, error) {
	channels := make([]slack.Channel, 0)
	var cursor string
	for {
		chs, nextCursor, err := client.GetConversations(&slack.GetConversationsParameters{
			Cursor:          cursor,
			ExcludeArchived: true,
			Types:           []string{"public_channel"},
		})
		if err != nil {
			return nil, errors.Wrap(err, "GetConversations")
		}
		channels = append(channels, chs...)

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return channels, nil
}

func saveChannels(client *slack.Client) error {
	channels, err := listChannels(client)
	if err != nil {
		return err
	}

	return saveJSON(channelsJSONPath, channels)
}

func loadChannels(client *slack.Client) ([]slack.Channel, error) {
	// Read channels from file
	bb, err := os.ReadFile(channelsJSONPath)
	// If not exist, fetch channels and save/load it
	if errors.Is(err, os.ErrNotExist) {
		log.Infof("'%s' is not exist", channelsJSONPath)
		if err := saveChannels(client); err != nil {
			return nil, err
		}
		return loadChannels(client)
	}
	if err != nil {
		return nil, err
	}

	var channels []slack.Channel
	if err := json.Unmarshal(bb, &channels); err != nil {
		return nil, err
	}
	log.Infof("%d channels are loaded from json file", len(channels))
	return channels, nil
}

func run(client *slack.Client, channels []slack.Channel) error {
	for _, channel := range channels {
		entry := log.WithField("channel", "#"+channel.Name)

		// 혹시 아카이브된 채널이면 pass
		if channel.IsArchived {
			entry.Info("skipped archived channel")
			continue
		}

		// 스크립트 특성상 ctrl+c로 중단했다 다시 실행하는 경우가 잦은데 이때
		// 이미 메시지를 불러온 채널이라면 pass
		path := fmt.Sprintf("raw/%s.json", channel.Name)
		if _, err := os.Stat(path); err == nil {
			entry.Info("already saved")
			continue
		}

		msgs, err := listMessages(client, channel, days30Ago)
		// 채널에 들어가있지 않더라도 불러올 수야 있지만 혹시몰라 하는 에러 핸들링
		if errors.Is(err, errNotInChannel) {
			entry.Error("not in channel")
			continue
		}
		if err != nil {
			return err
		}

		entry.Infof("fetched %d messages", len(msgs))
		if err := saveJSON(path, removeBlocks(msgs)); err != nil {
			return errors.Wrapf(err, "channel: %s", channel.Name)
		}
	}
	return nil
}

func listMessages(client *slack.Client, channel slack.Channel, until time.Time) ([]slack.Message, error) {
	messages := make([]slack.Message, 0, 200)
	cursor := ""
	for {
		resp, err := client.GetConversationHistory(&slack.GetConversationHistoryParameters{
			ChannelID: channel.ID,
			Cursor:    cursor,
			Oldest:    strconv.FormatInt(until.Unix(), 10),
		})
		// api 호출이 너무 잦아 rate limit에 걸리면 잠시 대기
		if rateLimitedError, ok := err.(*slack.RateLimitedError); ok {
			log.WithField("channel", "#"+channel.Name).Error(err)
			time.Sleep(rateLimitedError.RetryAfter)
			continue
		}
		if err != nil {
			if err.Error() == "not_in_channel" {
				return nil, errNotInChannel
			}
			return nil, err
		}

		msgsToAppend := make([]slack.Message, 0, len(resp.Messages))
		for _, message := range resp.Messages {
			// "thread_broadcast" 타입은 본문 스레드에서 가져올 수 있어 넘어감
			if message.SubType == "channel_join" ||
				message.SubType == "channel_leave" ||
				message.SubType == "thread_broadcast" {
				continue
			}
			// 위에선 스레드 메시지가 아니라 채널 메시지만 가져오기에 만약 스레드가 있다면 별도로 가져와줘야함
			if message.ReplyCount > 0 {
				// 잠깐의 여.유.
				time.Sleep(1 * time.Second)
				thread, err := listMessagesInThread(client, channel, message.Timestamp)
				if err != nil {
					return nil, err
				}
				msgsToAppend = append(msgsToAppend, thread...)
			} else { // 스레드 본문 메시지는 위 응답에 같이 딸려오기에 두번 추가하지 않음
				msgsToAppend = append(msgsToAppend, message)
			}
		}
		messages = append(messages, msgsToAppend...)

		if !resp.HasMore || resp.ResponseMetadata.Cursor == "" {
			break
		}

		cursor = resp.ResponseMetadata.Cursor
		// 잠깐의 여.유.
		time.Sleep(1 * time.Second)
	}
	return messages, nil
}

func listMessagesInThread(client *slack.Client, channel slack.Channel, ts string) ([]slack.Message, error) {
	messages := make([]slack.Message, 0, 100)
	cursor := ""
	for {
		resp, hasMore, nextCursor, err := client.GetConversationReplies(&slack.GetConversationRepliesParameters{
			ChannelID: channel.ID,
			Timestamp: ts,
			Cursor:    cursor,
			// 원래 스레드의 메시지도 최근 n일 이내인지 검사해야하나 굳이 엄밀하지 않아도 되기에 그러지 않음
			Oldest: "",
		})
		// api 호출이 너무 잦아 rate limit에 걸리면 잠시 대기
		if rateLimitedError, ok := err.(*slack.RateLimitedError); ok {
			log.WithField("channel", "#"+channel.Name).Error(err)
			time.Sleep(rateLimitedError.RetryAfter)
			continue
		}
		if err != nil {
			return nil, err
		}
		messages = append(messages, resp...)

		if !hasMore || nextCursor == "" {
			break
		}

		cursor = nextCursor
		// 잠깐의 여.유.
		time.Sleep(1 * time.Second)
	}
	return messages, nil
}

// 메시지 안 "blocks" 필드가 너무 길고 굳이 필요하지 않아 삭제함
func removeBlocks(msgs []slack.Message) []slack.Message {
	removed := make([]slack.Message, len(msgs))
	for i, msg := range msgs {
		msg.Blocks = slack.Blocks{BlockSet: nil}
		removed[i] = msg
	}
	return removed
}
