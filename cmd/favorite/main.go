package main

import (
	"encoding/json"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	channelMessageJSONDirPath = "data"
)

var (
	// 메시지 텍스트내 :+1:, :smile: 이런 이모지를 추출하기 위한 정규표현식
	emojiPattern = regexp.MustCompile(`:([가-힣a-zA-Z\d-_+]+):`)
)

func main() {
	slackBotToken := os.Getenv("SLACK_BOT_TOKEN")

	client := slack.New(slackBotToken)
	if _, err := client.AuthTest(); err != nil {
		log.Fatal(err)
	}

	// 한 번도 사용하지 않은 이모지를 찾음
	if err := favorite(client); err != nil {
		log.Fatal(err)
	}

	if err := merge(); err != nil {
		log.Fatal(err)
	}
}

type row struct {
	Emoji map[string]int `json:"emoji"`
	User  slackUser      `json:"user"`
}

// 수동으로 편집한 결과와 emoji:link 맵을 합쳐 html 생성
func merge() error {
	bb, err := os.ReadFile("favorite_edited.json")
	if err != nil {
		return err
	}
	var rows []row
	if err := json.Unmarshal(bb, &rows); err != nil {
		return err
	}

	bb, err = os.ReadFile("favorite_map.json")
	if err != nil {
		return err
	}
	var emojiLink map[string]string
	if err := json.Unmarshal(bb, &emojiLink); err != nil {
		return err
	}

	var text string
	for _, data := range rows {
		text += "<div>\n"
		text += fmt.Sprintf(`  <img src="%s"><span>%s</span>`+"\n", data.User.Image, data.User.Name)
		// 많이 쓴 순서로 정렬하기 위한 slice
		names := make([]string, 0, 3)
		for name := range data.Emoji {
			names = append(names, name)
		}
		sort.Slice(names, func(i, j int) bool {
			return data.Emoji[names[i]] > data.Emoji[names[j]]
		})
		for _, name := range names {
			count := data.Emoji[name]
			link := emojiLink[name]
			text += fmt.Sprintf(`  <img src="%s" alt="%s"><span>%d번</span>`+"\n", link, name, count)
		}
		text += "</div>\n"
	}
	return os.WriteFile("output.html", []byte(text), 0644)
}

func favorite(client *slack.Client) error {
	msgs, err := loadChannelMessages()
	if err != nil {
		return err
	}

	// counter: map[유저ID]map[이모지]사용횟수
	counter := make(map[string]map[string]int)
	for _, msg := range msgs {
		if msg.User == "" {
			continue
		}
		// 봇이 보낸건 무시
		if msg.BotID != "" {
			continue
		}
		for user, emojiMap := range countEmojiUsageByUserFromMessage(msg) {
			// 처음이면 초기화
			if _, ok := counter[user]; !ok {
				counter[user] = map[string]int{}
			}
			for emoji, count := range emojiMap {
				counter[user][emoji] += count
			}
		}
	}

	counter = rankTop3ByUser(counter)
	logEmojis(counter)

	userMap, err := makeUserMap(client)
	if err != nil {
		return err
	}

	if err := saveJSON("favorite.json", convertUserNameOfCounter(userMap, counter)); err != nil {
		return err
	}
	return nil
}

// 복사용 출력
func logEmojis(counter map[string]map[string]int) {
	emojiMap := map[string]struct{}{}
	for _, usage := range counter {
		for emoji := range usage {
			emojiMap[emoji] = struct{}{}
		}
	}
	emojis := make([]string, 0)
	for emoji := range emojiMap {
		emojis = append(emojis, fmt.Sprintf(":%s:", emoji))
	}
	log.Info(strings.Join(emojis, "\n"))
}

type slackUser struct {
	slack     slack.User
	Id        string `json:"id"`
	Image     string `json:"image"`
	Name      string `json:"name"`
	SlackName string
}

// 슬랙 ID를 이름으로 변환하기 위한 map
func makeUserMap(client *slack.Client) (map[string]slackUser, error) {
	slackUsers, err := client.GetUsers()
	if err != nil {
		return nil, err
	}

	userMap := make(map[string]slackUser, len(slackUsers))
	for _, user := range slackUsers {
		// 퇴사자
		if user.RealName == "" {
			continue
		}
		if user.IsBot {
			continue
		}

		name := user.RealName
		// 만약 "길동 홍"으로 되어있으면 "홍길동"로 바꿔줌
		if names := strings.Split(name, " "); len(names) > 1 {
			name = names[1] + names[0]
		}
		userMap[user.ID] = slackUser{
			slack:     user,
			Id:        user.ID,
			Image:     user.Profile.Image192,
			Name:      name,
			SlackName: user.RealName,
		}
	}
	return userMap, nil
}

func normalize(s string) string {
	t := transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, err := transform.String(t, s)
	if err != nil {
		log.WithError(err).Errorf("cannot normalize '%s'", s)
		return s
	}

	return result
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

func countEmojiUsageByUserFromMessage(m slack.Message) map[string]map[string]int {
	counter := map[string]map[string]int{
		m.User: {},
	}
	// extract from text
	for _, name := range extractEmojisFromText(m.Text) {
		counter[m.User][name] += 1
	}
	// extract from reactions
	for _, r := range m.Reactions {
		for _, user := range r.Users {
			// 처음이면 초기화
			if _, ok := counter[user]; !ok {
				counter[user] = map[string]int{}
			}
			name := removeSkinTone(normalize(r.Name))
			counter[user][name] += 1
		}
	}
	return counter
}

// "+1::skin-tone-3" to "+1"
func removeSkinTone(s string) string {
	return strings.Split(s, "::")[0]
}

func extractEmojisFromText(s string) []string {
	matched := emojiPattern.FindAllStringSubmatch(s, -1)
	if len(matched) == 0 {
		return nil
	}

	emojis := make([]string, 0, len(matched))
	for _, match := range matched {
		name := normalize(match[1]) // if matched, length is at least 2
		// 메시지에서 사용한 :pray::skin-tone-2: 같은 경우 :pary:로만 카운팅하면 충분하기에 스킨톤은 걸러줌
		if strings.HasPrefix(name, "skin-tone-") {
			continue
		}
		// "2020-02-01 00:00:00" 이런게 파싱돼 :00:이 이모지로 인식되는데 현재 뾰족한 수가 없어 결과 데이터를 보고
		// 하드코딩해서 이모지로 인식안되게 거름
		if name == "00" || name == "23" || name == "49" {
			continue
		}
		emojis = append(emojis, name)
	}
	return emojis
}

func rankTop3ByUser(counter map[string]map[string]int) map[string]map[string]int {
	m := make(map[string]map[string]int)
	for user, emojiMap := range counter {
		if len(emojiMap) == 0 {
			continue
		}
		emojis := make([]string, 0, len(emojiMap))
		for emoji := range emojiMap {
			emojis = append(emojis, emoji)
		}
		// 사용 횟수가 많은 순서로 정렬
		sort.Slice(emojis, func(i, j int) bool {
			return emojiMap[emojis[i]] > emojiMap[emojis[j]]
		})

		top3 := make(map[string]int, 3)
		for _, emoji := range emojis[:min(len(emojis), 3)] {
			top3[emoji] = emojiMap[emoji]
		}
		m[user] = top3
	}
	return m
}

func convertUserNameOfCounter(userMap map[string]slackUser, counter map[string]map[string]int) []map[string]interface{} {
	result := make([]map[string]interface{}, 0)
	for slackID, emojiMap := range counter {
		user, ok := userMap[slackID]
		// 퇴사자인 경우
		if !ok {
			continue
		}
		result = append(result, map[string]interface{}{
			"user":  user,
			"emoji": emojiMap,
		})
	}
	return result
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
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
