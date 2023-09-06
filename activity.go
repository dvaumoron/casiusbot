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

import "time"

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

func bgManageActivity(activityPath string, saveInterval time.Duration) chan<- memberActivity {
	activityChannel := make(chan memberActivity)
	go manageActivity(activityChannel, activityPath, saveInterval)
	return activityChannel
}

func manageActivity(activityChannelReceiver <-chan memberActivity, activityPath string, saveInterval time.Duration) {
	saveticker := time.Tick(saveInterval)
	activities := map[string]activityData{}
	// TODO read saved data
	for {
		select {
		case mActivity := <-activityChannelReceiver:
			activity := activities[mActivity.userId]
			activity.messageCount++
			if mActivity.vocal {
				activity.lastVocal = mActivity.timestamp
			} else {
				activity.lastMessage = mActivity.timestamp
			}
			activities[mActivity.userId] = activity
		case <-saveticker:
			// TODO write data to a csv file
		}
	}
}
