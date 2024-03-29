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
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/dvaumoron/casiusbot/common"
)

func buildReminderPrefix(config common.Config, reminderConfName string, guildId string) string {
	var reminderBuilder strings.Builder
	reminderBuilder.WriteString(config.Require(reminderConfName))
	reminderBuilder.WriteString("\nhttps://discord.com/events/")
	reminderBuilder.WriteString(guildId)
	reminderBuilder.WriteByte('/')
	return reminderBuilder.String()
}

func remindEvent(session *discordgo.Session, guildId string, delays []time.Duration, messageSender chan<- common.MultipartMessage, reminderPrefix string, previous time.Time, ticker <-chan time.Time) {
	for current := range ticker {
		events, err := session.GuildScheduledEvents(guildId, false)
		if err != nil {
			log.Println("Cannot retrieve guild events :", err)
			continue
		}

		for _, event := range events {
			eventStartTime := event.ScheduledStartTime
			for _, delay := range delays {
				// delay  is already negative
				reminderTime := eventStartTime.Add(delay)
				if reminderTime.After(previous) && reminderTime.Before(current) {
					messageSender <- common.MultipartMessage{Message: reminderPrefix + event.ID}
					// don't test other delay
					break
				}
			}
		}
		previous = current
	}
}
