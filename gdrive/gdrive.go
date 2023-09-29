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

package gdrive

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/dvaumoron/casiusbot/common"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

const configMsg = "Google Drive configuration initialization failed :"

type DriveConfig struct {
	authConfig    *oauth2.Config
	tokenPath     string
	token         *oauth2.Token
	followLinkMsg string
	importFormats map[string][]string
}

func ReadConfig(credentialsPath string, tokenPath string, followLinkMsg string) DriveConfig {
	credentialsData, err := os.ReadFile(credentialsPath)
	if err != nil {
		panic(fmt.Sprint(configMsg, err))
	}

	// If modifying these scopes, delete your previously saved token.json.
	authConfig, err := google.ConfigFromJSON(credentialsData, drive.DriveScope)
	if err != nil {
		panic(fmt.Sprint(configMsg, err))
	}

	var token *oauth2.Token
	file, err := os.Open(tokenPath)
	if err == nil {
		defer file.Close()

		token = &oauth2.Token{}
		if err = json.NewDecoder(file).Decode(token); err != nil {
			token = nil // ignore error and force a token generation on send to google drive
		}
	}

	config := DriveConfig{authConfig: authConfig, tokenPath: tokenPath, token: token, followLinkMsg: followLinkMsg}
	if token != nil {
		srv, err := config.newService()
		if err == nil {
			config.initImportFormats(srv) // ignore error
		}
	}
	return config
}

func (config *DriveConfig) newService() (*drive.Service, error) {
	ctx := context.Background()
	return drive.NewService(ctx, option.WithHTTPClient(config.authConfig.Client(ctx, config.token)))
}

func (config *DriveConfig) initImportFormats(srv *drive.Service) error {
	if len(config.importFormats) == 0 {
		about, err := srv.About.Get().Fields("importFormats").Do()
		if err != nil {
			return err
		}
		config.importFormats = about.ImportFormats
	}
	return nil
}

func (config *DriveConfig) CreateDriveSender(driveFolderId string, errorMsgSender chan<- common.MultipartMessage) chan<- common.MultipartMessage {
	messageChan := make(chan common.MultipartMessage)
	go config.sendFileToDrive(driveFolderId, messageChan, errorMsgSender)
	return messageChan
}

func (config *DriveConfig) sendFileToDrive(driveFolderId string, dataReceiver <-chan common.MultipartMessage, errorMsgSender chan<- common.MultipartMessage) {
	for multiMessage := range dataReceiver {
		if config.token == nil {
			config.sendRefreshUrl(errorMsgSender)
			continue
		}

		srv, err := config.newService()
		if err != nil {
			log.Println("Unable to access Drive API :", err)
			continue
		}

		if err = config.initImportFormats(srv); err != nil {
			config.manageError(errorMsgSender, err, "Unable to retrieve import formats :", multiMessage.ErrorMsg)
			continue
		}

		mimeType := ""
		if dotIndex := strings.LastIndexByte(multiMessage.FileName, '.'); dotIndex != -1 {
			mimeType = mime.TypeByExtension(multiMessage.FileName[dotIndex:])
		} else {
			mimeType = http.DetectContentType([]byte(multiMessage.FileData))
		}

		conversionMimeType := ""
		if formats := config.importFormats[cleanMimeType(mimeType)]; len(formats) != 0 {
			conversionMimeType = formats[0]
		}

		_, err = srv.Files.Create(
			&drive.File{
				Parents:  []string{driveFolderId},
				Name:     multiMessage.FileName,
				MimeType: conversionMimeType,
			},
		).Media(strings.NewReader(multiMessage.FileData), googleapi.ContentType(mimeType)).Do()
		if err != nil {
			config.manageError(errorMsgSender, err, "Unable to create file in Drive :", multiMessage.ErrorMsg)
		}
	}
}

func (config *DriveConfig) manageError(errorMsgSender chan<- common.MultipartMessage, err error, logMsg string, userErrorMsg string) {
	if errMsg := err.Error(); strings.Contains(errMsg, "invalid_grant") {
		config.sendRefreshUrl(errorMsgSender)
	} else {
		log.Println(logMsg, errMsg)
		errorMsgSender <- common.MultipartMessage{Message: userErrorMsg}
	}
}

func (config *DriveConfig) sendRefreshUrl(errorMsgSender chan<- common.MultipartMessage) {
	authURL := config.authConfig.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	errorMsgSender <- common.MultipartMessage{Message: strings.ReplaceAll(config.followLinkMsg, "{{link}}", authURL)}
}

func (config *DriveConfig) DriveTokenCmdEffect(i *discordgo.InteractionCreate, msgs common.Messages) string {
	if options := i.ApplicationCommandData().Options; len(options) != 0 {
		if err := config.saveToken(options[0].StringValue()); err == nil {
			return msgs.Ok
		} else {
			log.Println("Unable to save Google Drive token :", err)
		}
	}
	return msgs.ErrGlobal
}

// Write the token retrieved from browser in a file.
func (config *DriveConfig) saveToken(authCode string) error {
	token, err := config.authConfig.Exchange(context.Background(), authCode)
	if err != nil {
		return err
	}
	config.token = token

	file, err := os.OpenFile(config.tokenPath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	return json.NewEncoder(file).Encode(token)
}

func cleanMimeType(mimetype string) string {
	if index := strings.IndexByte(mimetype, ';'); index != -1 {
		return mimetype[:index]
	}
	return mimetype
}
