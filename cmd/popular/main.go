package main

import (
	"encoding/json"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	"os"
	"path/filepath"
	"sort"
)

const (
	channelMessageJSONDirPath = "data"
)

func main() {
	// 가장 반응이 뜨거운 메시지를 찾음
	if err := popular(); err != nil {
		log.Fatal(err)
	}
}

type slackMsg struct {
	Msg   slack.Message `json:"msg"`
	Count int           `json:"count"`
}

func (m slackMsg) String() string {
	return fmt.Sprintf("%s (%d)", m.Msg.Timestamp, m.Count)
}

func popular() error {
	msgs, err := loadChannelMessages()
	if err != nil {
		return err
	}

	slackMsgs := make([]slackMsg, len(msgs))
	for i, m := range msgs {
		slackMsgs[i] = countReactions(m)
	}

	sort.Slice(slackMsgs, func(i, j int) bool {
		return slackMsgs[i].Count > slackMsgs[j].Count
	})

	if err := saveJSON("popular.json", slackMsgs[:10]); err != nil {
		return err
	}
	return nil
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

func countReactions(m slack.Message) slackMsg {
	var count int
	for _, r := range m.Reactions {
		count += r.Count
	}
	return slackMsg{
		Msg:   m,
		Count: count,
	}
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
