# CasiusBot

A [Discord](https://discord.com/) bot, with the following features :
- add a prefix on nickname based on the user's roles (with priority to some "special" roles).
- add a default role to user without any prefix role (except for user with forbidden roles)
- add a role on joining user (could be the default or a forbidden role)
- add a set of command allowing user to choose a prefix role and one to reset to default role (those command does not work for user with forbidden roles)
- add a command to display a count of users by role 
- post reminder messages for scheduled events

Optionally (when corresponding configuration is present) :
- randomly change its game status
- check regularly [RSS](https://www.rssboard.org/rss-specification) feeds and send messages with the links in a channel (can filter link with [regexp](https://en.wikipedia.org/wiki/Regular_expression) or translate an extract (call [DeepL API](https://www.deepl.com/)))