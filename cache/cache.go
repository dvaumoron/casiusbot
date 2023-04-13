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
package cache

import "container/list"

type empty = struct{}

type Cache struct {
	maxSize int
	linked  *list.List
	set     map[string]empty
}

func Make(maxSize int) Cache {
	return Cache{maxSize: maxSize, linked: list.New(), set: map[string]empty{}}
}

func (c Cache) Contains(elem string) bool {
	_, ok := c.set[elem]
	return ok
}

func (c Cache) Add(elem string) bool {
	_, ok := c.set[elem]
	if !ok {
		c.set[elem] = empty{}
		c.linked.PushFront(elem)
		c.manageSize()
	}
	return ok
}

func (c Cache) Init(elems []string) {
	for _, elem := range elems {
		c.set[elem] = empty{}
		c.linked.PushBack(elem)
	}
	c.manageSize()
}

func (c Cache) manageSize() {
	for c.linked.Len() > c.maxSize {
		delete(c.set, c.linked.Remove(c.linked.Back()).(string))
	}
}
