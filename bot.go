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
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

const errUserMsg = "Hey there ! I got a problem trying to execute the apply-prefix command."

func main() {
	if godotenv.Overload() == nil {
		fmt.Println("Loaded .env file")
	}

	session, err := discordgo.New("Bot " + os.Getenv("BOT_TOKEN"))
	if err != nil {
		fmt.Println("An error occured :", err)
	}

	session.Identify.Intents |= discordgo.IntentGuildMembers

	cmd := &discordgo.ApplicationCommand{
		Name:        "apply-prefix",
		Description: "Apply the prefix rule to all User",
	}

	filePath := os.Getenv("PREFIX_FILE_PATH")
	nameToPrefix, err := readPrefixConfig(filePath)
	if err != nil {
		log.Fatalln("Cannot open the configuration file :", err)
	}

	prefixes := make([]string, 0, len(nameToPrefix))
	for _, prefix := range nameToPrefix {
		prefixes = append(prefixes, prefix)
	}

	guildId := os.Getenv("GUILD_ID")

	err = session.Open()
	if err != nil {
		log.Fatalln("Cannot open the session :", err)
	}
	defer session.Close()

	guildRoles, err := session.GuildRoles(guildId)
	if err != nil {
		log.Println("Cannot retrieve roles of the guild :", err)
		return
	}

	roleIdToPrefix := map[string]string{}
	for _, guildRole := range guildRoles {
		if prefix, ok := nameToPrefix[guildRole.Name]; ok {
			roleIdToPrefix[guildRole.ID] = prefix
		}
	}

	session.AddHandler(func(s *discordgo.Session, u *discordgo.GuildMemberUpdate) {
		log.Println("Member update detected")
		nickName := u.Member.Nick
		if nickName == "" {
			nickName = u.User.Username
		}

		newNickName := transformName(nickName, u.Roles, roleIdToPrefix, prefixes)
		if newNickName != nickName {
			if err = s.GuildMemberNickname(u.GuildID, u.User.ID, newNickName); err != nil {
				log.Println("An error occurred (1) :", err)
			}
		}
	})

	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.ApplicationCommandData().Name == cmd.Name {
			guildMembers, err := s.GuildMembers(i.GuildID, "", 1000)
			returnMsg := "Hey there ! Congratulations, you have just executed the apply-prefix command."
			if err == nil {
				for _, guildMember := range guildMembers {
					nickName := guildMember.Nick
					if nickName == "" {
						nickName = guildMember.User.Username
					}

					newNickName := transformName(nickName, guildMember.Roles, roleIdToPrefix, prefixes)
					if newNickName != nickName {
						if err = s.GuildMemberNickname(i.GuildID, guildMember.User.ID, newNickName); err != nil {
							log.Println("An error occurred (2) :", err)
							returnMsg = errUserMsg
							break
						}
					}
				}
			} else {
				log.Println("An error occurred (3) :", err)
				returnMsg = errUserMsg
			}

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: returnMsg},
			})
		}
	})

	appId := session.State.User.ID
	cmd, err = session.ApplicationCommandCreate(appId, guildId, cmd)
	if err != nil {
		log.Println("Cannot create command :", err)
		return
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Println("Press Ctrl+C to exit")
	<-stop

	if err = session.ApplicationCommandDelete(appId, guildId, cmd.ID); err != nil {
		log.Println("Cannot delete command :", err)
	}
}

func readPrefixConfig(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	nameToPrefix := map[string]string{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) != 0 && line[0] != '#' {
			sepIndex := strings.IndexByte(line, '=')
			nameToPrefix[strings.TrimSpace(line[:sepIndex])] = strings.TrimSpace(line[sepIndex+1:]) + " "
		}
	}
	return nameToPrefix, nil
}

func transformName(nickName string, roleIds []string, roleIdToPrefix map[string]string, prefixes []string) string {
	nickName = cleanPrefix(nickName, prefixes)
	for _, roleId := range roleIds {
		if prefix, ok := roleIdToPrefix[roleId]; ok {
			// prefix already end with a space
			return prefix + nickName
		}
	}
	return nickName
}

func cleanPrefix(nickName string, prefixes []string) string {
	for _, prefix := range prefixes {
		if strings.HasPrefix(nickName, prefix) {
			return nickName[len(prefix):]
		}
	}
	return nickName
}
