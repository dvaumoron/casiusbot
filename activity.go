/*
 *
 * Copyright 2023 casiusbot authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package main

import (
	"encoding/csv"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/dvaumoron/casiusbot/common"
)

type memberActivity struct {
	userId    string
	timestamp time.Time
	vocal     bool
}
type activityData struct {
	messageCount int
	lastMessage  time.Time
	lastVocal    time.Time
}

func bgManageActivity(session *discordgo.Session, saveTickReceiver <-chan bool, dataSender chan<- common.MultipartMessage, activityPath string, dateFormat string, cmdName string, infos common.GuildAndConfInfo) chan<- memberActivity {
	activityChannel := make(chan memberActivity)
	go manageActivity(session, saveTickReceiver, dataSender, activityPath, dateFormat, cmdName, infos, activityChannel)
	return activityChannel
}

func manageActivity(session *discordgo.Session, saveTickReceiver <-chan bool, dataSender chan<- common.MultipartMessage, activityPath string, dateFormat string, cmdName string, infos common.GuildAndConfInfo, activityChannelReceiver <-chan memberActivity) {
	activities := loadActivities(activityPath, dateFormat)
	activityFileName := filepath.Base(activityPath)
	errorMsg := strings.ReplaceAll(infos.Msgs.ErrGlobalCmd, common.CmdPlaceHolder, cmdName)
	for {
		select {
		case mActivity := <-activityChannelReceiver:
			activity := activities[mActivity.userId]
			if mActivity.vocal {
				activity.lastVocal = mActivity.timestamp
			} else {
				activity.messageCount++
				activity.lastMessage = mActivity.timestamp
			}
			activities[mActivity.userId] = activity
		case sendFile := <-saveTickReceiver:
			var builder strings.Builder
			writer := csv.NewWriter(&builder)
			// header
			writer.Write([]string{"userId", "userName", "userNickname", "messageCount", "lastMessage", "lastVocal", "lastActivity"})
			for _, idNames := range loadMemberIdAndNames(session, infos) {
				activity := activities[idNames[0]]

				lastMessage := activity.lastMessage.Format(dateFormat)
				lastVocal := activity.lastVocal.Format(dateFormat)
				lastActivity := lastMessage
				if activity.lastMessage.Before(activity.lastVocal) {
					lastActivity = lastVocal
				}

				writer.Write([]string{idNames[0], idNames[1], idNames[2], strconv.Itoa(activity.messageCount), lastMessage, lastVocal, lastActivity})
			}
			writer.Flush()
			data := builder.String()

			if sendFile {
				dataSender <- common.MultipartMessage{FileName: activityFileName, FileData: data, ErrorMsg: errorMsg}
			}
			os.WriteFile(activityPath, []byte(data), 0o644)
		}
	}
}

func loadActivities(activityPath string, dateFormat string) map[string]activityData {
	activities := map[string]activityData{}
	file, err := os.Open(activityPath)
	if err != nil {
		log.Println("Loading saved activities failed :", err)
		return activities
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Read() // skip header
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Println("Parsing saved activities failed :", err)
			return map[string]activityData{} // after error, restart monitoring with empty data
		}

		messageCount, err := strconv.Atoi(record[3])
		if err != nil {
			log.Println("Parsing message count failed :", err)
			return map[string]activityData{} // after error, restart monitoring with empty data
		}
		lastMessage, err := time.Parse(dateFormat, record[4])
		if err != nil {
			log.Println("Parsing last message date failed :", err)
			return map[string]activityData{} // after error, restart monitoring with empty data
		}
		lastVocal, err := time.Parse(dateFormat, record[5])
		if err != nil {
			log.Println("Parsing last vocal date failed :", err)
			return map[string]activityData{} // after error, restart monitoring with empty data
		}

		activities[record[0]] = activityData{
			messageCount: messageCount, lastMessage: lastMessage, lastVocal: lastVocal,
		}
	}
	return activities
}

func loadMemberIdAndNames(session *discordgo.Session, infos common.GuildAndConfInfo) [][3]string {
	guildMembers, err := session.GuildMembers(infos.GuildId, "", common.MemberCallLimit)
	if err != nil {
		log.Println("Cannot retrieve guild members (4) :", err)
		return nil
	}

	names := make([][3]string, 0, len(guildMembers))
	for _, member := range guildMembers {
		names = append(names, [3]string{
			member.User.ID,
			member.User.Username,
			common.ExtractNick(member),
		})
	}
	return names
}
