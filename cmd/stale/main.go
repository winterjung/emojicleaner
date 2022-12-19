package main

import (
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/slack-go/slack"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	log "github.com/sirupsen/logrus"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	emojisJSONPath            = "emojis.json"
	channelMessageJSONDirPath = "data"
)

var (
	// 메시지 텍스트내 :+1:, :smile: 이런 이모지를 추출하기 위한 정규표현식
	emojiPattern     = regexp.MustCompile(`:([가-힣a-zA-Z\d-_+]+):`)
	emojiNamePattern = regexp.MustCompile(`[가-힣a-zA-Z\d-_+]+`)
)

func main() {
	// 한 번도 사용하지 않은 이모지를 찾음
	if err := stale(); err != nil {
		log.Fatal(err)
	}
}

type emoji struct {
	Name     string `json:"name"`
	Link     string `json:"link,omitempty"`
	IsCustom bool   `json:"is_custom"`
	Count    int    `json:"count"`
}

func (e emoji) String() string {
	return fmt.Sprintf(":%s:(%d)", e.Name, e.Count)
}

func stale() error {
	emojis, err := loadEmojis()
	if err != nil {
		return err
	}
	if err := checkAllEmojiAreValid(emojis); err != nil {
		return err
	}
	customEmojiMap := makeEmojiMap(emojis)

	msgs, err := loadChannelMessages()
	if err != nil {
		return err
	}

	counter := make(map[string]int)
	for _, msg := range msgs {
		for name, count := range countRawEmojisFromMessage(msg) {
			counter[name] += count
		}
	}

	emojis = merge(customEmojiMap, counter)
	log.Infof("totally %d emojis are used", len(counter))
	unused := listUnusedEmojis(emojis)
	log.Infof("%d emojis are unused", len(unused))

	if err := saveJSON("all_emojis.json", emojis); err != nil {
		return err
	}
	if err := saveJSON("unused_emojis.json", unused); err != nil {
		return err
	}
	return nil
}

func loadEmojis() ([]emoji, error) {
	bb, err := os.ReadFile(emojisJSONPath)
	if err != nil {
		return nil, err
	}

	v := make(map[string]string)
	if err := json.Unmarshal(bb, &v); err != nil {
		return nil, err
	}

	emojis := make([]emoji, 0, len(v))
	for name, link := range v {
		name := normalize(name)
		if strings.HasPrefix(name, "alphabet-") {
			continue
		}
		emojis = append(emojis, emoji{
			Name:     name,
			Link:     link,
			IsCustom: true,
		})
	}
	sort.Slice(emojis, func(i, j int) bool {
		return emojis[i].Name < emojis[j].Name
	})
	log.Infof("%d emojis are loaded from json file", len(emojis))
	return emojis, nil
}

func checkAllEmojiAreValid(ee []emoji) error {
	for _, e := range ee {
		if !emojiNamePattern.MatchString(e.Name) {
			return errors.Errorf("'%s' is not matched with emoji regexp", e.Name)
		}
	}
	return nil
}

func makeEmojiMap(ee []emoji) map[string]emoji {
	m := make(map[string]emoji)
	for _, e := range ee {
		m[e.Name] = e
	}
	return m
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

func countRawEmojisFromMessage(m slack.Message) map[string]int {
	emojiCount := make(map[string]int)
	// extract from text
	for _, name := range extractEmojisFromText(m.Text) {
		emojiCount[name] += 1
	}
	// extract from reactions
	for name, count := range extractFromReactions(m.Reactions) {
		emojiCount[name] += count
	}
	return emojiCount
}

func extractFromReactions(rr []slack.ItemReaction) map[string]int {
	emojiCount := make(map[string]int)
	for _, r := range rr {
		emojiCount[removeSkinTone(normalize(r.Name))] += r.Count
	}
	return emojiCount
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
		emojis = append(emojis, name)
	}
	return emojis
}

func merge(emojiMap map[string]emoji, counter map[string]int) []emoji {
	m := make(map[string]emoji)
	for name, count := range counter {
		// 커스텀 이모지인 경우 emojiMap에 존재
		if e, ok := emojiMap[name]; ok {
			m[name] = emoji{
				Name:     e.Name,
				Link:     e.Link,
				IsCustom: e.IsCustom,
				Count:    e.Count + count,
			}
		} else { // +1, smile 처럼 기본 제공 이모지인 경우
			m[name] = emoji{
				Name:     name,
				Link:     "",
				IsCustom: false,
				Count:    count,
			}
		}
	}
	emojis := make([]emoji, 0, len(m))
	for _, e := range m {
		emojis = append(emojis, e)
	}
	// 커스텀 이모지인데 1번도 사용되지 않은 경우
	for name, e := range emojiMap {
		if _, ok := m[name]; !ok {
			emojis = append(emojis, e)
		}
	}
	sort.Slice(emojis, func(i, j int) bool {
		return emojis[i].Count > emojis[j].Count
	})
	return emojis
}

func listUnusedEmojis(emojis []emoji) []emoji {
	ee := make([]emoji, 0, len(emojis))
	for _, e := range emojis {
		if e.Count == 0 {
			ee = append(ee, e)
		}
	}
	sort.Slice(ee, func(i, j int) bool {
		return ee[i].Name < ee[j].Name
	})
	return ee
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
