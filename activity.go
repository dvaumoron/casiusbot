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
	"bufio"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
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

func bgManageActivity(session *discordgo.Session, saveTickReceiver <-chan bool, targetChannelId string, cmdName string, activityPath string, dateFormat string, infos GuildAndConfInfo) chan<- memberActivity {
	dataSender := createDataSender(session, targetChannelId, infos.msgs[2], cmdName)
	activityChannel := make(chan memberActivity)
	go manageActivity(session, activityChannel, saveTickReceiver, dataSender, activityPath, dateFormat, infos)
	return activityChannel
}

func manageActivity(session *discordgo.Session, activityChannelReceiver <-chan memberActivity, saveTickReceiver <-chan bool, dataSender chan<- [2]string, activityPath string, dateFormat string, infos GuildAndConfInfo) {
	activities := loadActivities(activityPath, dateFormat)
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
			// header
			builder.WriteString("userId,userName,userNickname,messageCount,lastMessage,lastVocal\n")
			for _, idNames := range loadMemberIdAndNames(session, infos) {
				activity := activities[idNames[0]]

				// user id
				builder.WriteString(idNames[0])
				builder.WriteByte(',')
				// user name
				builder.WriteString(idNames[1])
				builder.WriteByte(',')
				// user nickname
				builder.WriteString(idNames[2])
				builder.WriteByte(',')
				builder.WriteString(strconv.Itoa(activity.messageCount))
				builder.WriteByte(',')
				builder.WriteString(activity.lastMessage.Format(dateFormat))
				builder.WriteByte(',')
				builder.WriteString(activity.lastVocal.Format(dateFormat))
				builder.WriteByte('\n')
			}
			data := builder.String()

			if sendFile {
				dataSender <- [2]string{activityPath, data}
			}
			os.WriteFile(activityPath, []byte(data), 0644)
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

	scanner := bufio.NewScanner(file)
	scanner.Scan() // skip header
	for scanner.Scan() {
		splitted := strings.Split(scanner.Text(), ",")
		messageCount, err := strconv.Atoi(splitted[3])
		if err != nil {
			log.Println("Parsing message count failed :", err)
			return map[string]activityData{} // after error, restart monitoring with empty data
		}

		lastMessage, err := time.Parse(dateFormat, splitted[4])
		if err != nil {
			log.Println("Parsing last message date failed :", err)
			return map[string]activityData{} // after error, restart monitoring with empty data
		}
		lastVocal, err := time.Parse(dateFormat, splitted[5])
		if err != nil {
			log.Println("Parsing last vocal date failed :", err)
			return map[string]activityData{} // after error, restart monitoring with empty data
		}
		activities[splitted[0]] = activityData{
			messageCount: messageCount, lastMessage: lastMessage, lastVocal: lastVocal,
		}
	}
	if err = scanner.Err(); err != nil {
		log.Println("Parsing saved activities failed :", err)
		return map[string]activityData{} // after error, restart monitoring with empty data
	}
	return activities
}

func loadMemberIdAndNames(session *discordgo.Session, infos GuildAndConfInfo) [][3]string {
	guildMembers, err := session.GuildMembers(infos.guildId, "", 1000)
	if err != nil {
		log.Println("Cannot retrieve guild members (4) :", err)
		return nil
	}

	names := make([][3]string, 0, len(guildMembers))
	for _, member := range guildMembers {
		userId := member.User.ID
		if userId != infos.ownerId && !idInSet(member.Roles, infos.adminitrativeRoleIds) {
			names = append(names, [3]string{userId, member.User.Username, extractNick(member)})
		}
	}
	return names
}

func userActivitiesCmd(s *discordgo.Session, i *discordgo.InteractionCreate, saveTickSender chan<- bool, infos GuildAndConfInfo) {
	returnMsg := infos.msgs[0]
	if idInSet(i.Member.Roles, infos.authorizedRoleIds) {
		saveTickSender <- true
	} else {
		returnMsg = infos.msgs[1]
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}
