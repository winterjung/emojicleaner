package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"unicode"
	"unicode/utf8"

	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
)

const (
	channelMessageJSONDirPath = "data"
)

var (
	codePattern    = regexp.MustCompile("(?s)(```.*?```)")
	urlPattern     = regexp.MustCompile(`(https?://[^|\s]+)`)
	slackIDPattern = regexp.MustCompile(`<(@|!subteam\^|#)([A-Z\d]+)(>|\|.+>)`)
	emojiPattern   = regexp.MustCompile(`:([가-힣a-zA-Z\d-_+]+):`)
)

func main() {
	// 가장 긴 메시지를 찾음
	if err := longest(); err != nil {
		log.Fatal(err)
	}
}

type slackMsg struct {
	Text             string `json:"text"`
	Length           int    `json:"length"`
	msg              slack.Message
	upperLetterCount int
	numberCount      int
	alphabetCount    int
}

func (m slackMsg) String() string {
	return fmt.Sprintf("%s (%d)", m.Text, m.Length)
}

func longest() error {
	msgs, err := loadChannelMessages()
	if err != nil {
		return err
	}

	slackMsgs := make([]slackMsg, 0, len(msgs))
	for _, m := range msgs {
		text := normalize(m.Text)
		length := utf8.RuneCountInString(text)
		// 기본적으로 1천자가 넘어야 긴걸로 인정
		if length < 1000 {
			continue
		}
		slackMsgs = append(slackMsgs, slackMsg{
			Text:             text,
			Length:           length,
			msg:              m,
			upperLetterCount: countUpperLetter(text),
			numberCount:      countNumber(text),
			alphabetCount:    countAlphabet(text),
		})
	}
	sort.Slice(slackMsgs, func(i, j int) bool {
		return slackMsgs[i].Length > slackMsgs[j].Length
	})

	log.Infof("total %d >1000 msgs are exist", len(slackMsgs))
	if err := saveJSON("longest.json", filterNonConversationMessages(slackMsgs)); err != nil {
		return err
	}
	return nil
}

func filterNonConversationMessages(msgs []slackMsg) []slackMsg {
	filtered := make([]slackMsg, 0, len(msgs))
	for _, msg := range msgs {
		if msg.msg.SubType == "bot_message" {
			continue
		}
		if ratio := msg.alphabetCount * 100 / msg.Length; ratio >= 40 {
			log.Warnf("영어 비율이 %d%%라 걸러짐", ratio)
			log.Info(msg.Text)
			continue
		}
		if ratio := msg.upperLetterCount * 100 / msg.Length; ratio >= 20 {
			log.Warnf("대문자 비율이 %d%%라 걸러짐", ratio)
			log.Info(msg.Text)
			continue
		}
		if ratio := msg.numberCount * 100 / msg.Length; ratio >= 20 {
			log.Warnf("숫자 비율이 %d%%라 걸러짐", ratio)
			log.Info(msg.Text)
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered
}

func countUpperLetter(s string) int {
	var count int
	for _, r := range s {
		if unicode.IsUpper(r) && unicode.IsLetter(r) {
			count += 1
		}
	}
	return count
}

func countNumber(s string) int {
	var count int
	for _, r := range s {
		if unicode.IsDigit(r) {
			count += 1
		}
	}
	return count
}

func countAlphabet(s string) int {
	var count int
	for _, r := range s {
		if ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') {
			count += 1
		}
	}
	return count
}

func normalize(s string) string {
	// 쓸데없이 긴 코드는 제거
	s = codePattern.ReplaceAllString(s, "")
	// URL 링크는 텍스트만 남겨두고 제거
	s = urlPattern.ReplaceAllString(s, "")
	// 슬랙 팀이나 유저 태그는 제거
	s = slackIDPattern.ReplaceAllString(s, "")
	// 이모지 제거
	s = emojiPattern.ReplaceAllString(s, "")
	return s
}

// 다해봐야 약 50MB라서 채널을 이용해 produce 하는 대신 한 번에 메모리로 로드함
func loadChannelMessages() ([]slack.Message, error) {
	dirs, err := os.ReadDir(channelMessageJSONDirPath)
	if err != nil {
		return nil, err
	}

	allMsgs := make([]slack.Message, 0, 4000)
	for _, dir := range dirs {
		bb, err := os.ReadFile(filepath.Join(channelMessageJSONDirPath, dir.Name()))
		if err != nil {
			return nil, err
		}

		msgs := make([]slack.Message, 0, 200)
		if err := json.Unmarshal(bb, &msgs); err != nil {
			return nil, err
		}
		allMsgs = append(allMsgs, msgs...)
	}

	log.Infof("%d messages are loaded from %d json files", len(allMsgs), len(dirs))
	return allMsgs, nil
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
