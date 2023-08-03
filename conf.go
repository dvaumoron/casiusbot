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
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

func requireConf(valueConfName string) string {
	value := strings.TrimSpace(os.Getenv(valueConfName))
	if value == "" {
		log.Fatalln("Configuration value is missing :", valueConfName)
	}
	return value
}

func getIdSet(namesConfName string, nameToId map[string]string) (map[string]empty, error) {
	names := os.Getenv(namesConfName)
	if names == "" {
		return nil, nil
	}
	idSet := map[string]empty{}
	for _, name := range strings.Split(names, ",") {
		name := strings.TrimSpace(name)
		id := nameToId[name]
		if id == "" {
			return nil, errors.New("Unrecognized name : " + name)
		}
		idSet[id] = empty{}
	}
	return idSet, nil
}

func getTrimmedSlice(valuesConfName string) []string {
	values := os.Getenv(valuesConfName)
	if values == "" {
		return nil
	}
	splitted := strings.Split(values, ",")
	for index, value := range splitted {
		splitted[index] = strings.TrimSpace(value)
	}
	return splitted
}

func getAndParseDurationSec(valueConfName string) time.Duration {
	value := os.Getenv(valueConfName)
	if value == "" {
		return 0
	}
	valueSec, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		log.Println("Configuration", valueConfName, "parsing failed :", err)
	}
	return time.Duration(valueSec) * time.Second
}

func getAndParseDelayMins(valuesConfName string) []time.Duration {
	values := strings.Split(os.Getenv(valuesConfName), ",")
	delays := make([]time.Duration, 0, len(values))
	for _, value := range values {
		valueMin, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			log.Fatalln("Configuration", valuesConfName, "parsing failed :", err)
		}
		delay := time.Duration(valueMin) * time.Minute
		if delay > 0 {
			delay = -delay
		}
		delays = append(delays, delay)
	}
	return delays
}
