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

func bgManageActivity(session *discordgo.Session, activityPath string, dateFormat string, saveInterval time.Duration, infos GuildAndConfInfo) chan<- memberActivity {
	activityChannel := make(chan memberActivity)
	go manageActivity(session, activityChannel, activityPath, dateFormat, saveInterval, infos)
	return activityChannel
}

func manageActivity(session *discordgo.Session, activityChannelReceiver <-chan memberActivity, activityPath string, dateFormat string, saveInterval time.Duration, infos GuildAndConfInfo) {
	saveticker := time.Tick(saveInterval)
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
		case <-saveticker:
			memberNames := loadMemberNames(session, infos.guildId)

			var builder strings.Builder
			// header
			builder.WriteString("userId,userName,userNickName,messageCount,lastMessage,lastVocal\n")
			for userId, name := range memberNames {
				activity := activities[userId]

				builder.WriteString(userId)
				builder.WriteByte(',')
				// user name
				builder.WriteString(name[0])
				builder.WriteByte(',')
				// user nickname
				builder.WriteString(name[1])
				builder.WriteByte(',')
				builder.WriteString(strconv.Itoa(activity.messageCount))
				builder.WriteByte(',')
				builder.WriteString(activity.lastMessage.Format(dateFormat))
				builder.WriteByte(',')
				builder.WriteString(activity.lastVocal.Format(dateFormat))
				builder.WriteByte('\n')
			}
			os.WriteFile(activityPath, []byte(builder.String()), 0644)
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

func loadMemberNames(session *discordgo.Session, guildId string) map[string][2]string {
	names := map[string][2]string{}
	guildMembers, err := session.GuildMembers(guildId, "", 1000)
	if err != nil {
		log.Println("Cannot retrieve guild members (4) :", err)
		return names
	}
	for _, guildMember := range guildMembers {
		names[guildMember.User.ID] = [2]string{guildMember.User.Username, guildMember.Nick}
	}
	return names
}

func userActivitiesCmd(s *discordgo.Session, i *discordgo.InteractionCreate, sender pathSender, activityPath string, infos GuildAndConfInfo) {
	returnMsg := infos.msgs[0]
	if idInSet(i.Member.Roles, infos.authorizedRoleIds) {
		sender.SendPath(activityPath)
	} else {
		returnMsg = infos.msgs[1]
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}
