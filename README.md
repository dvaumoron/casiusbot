# CasiusBot

A [Discord](https://discord.com/) bot, with the following features :

- add a prefix on nickname based on the user's roles (with priority to some "special" roles).
- add a default role to user without any prefix role (except for user with forbidden roles)
- add a set of command allowing user to choose a prefix role and one to reset to default role (those command does not work for user with forbidden roles)
- post reminder messages for scheduled events

Optionally (when corresponding configuration is present) :

- add a role on joining user (could be the default, a prefix role or a forbidden role)
- add a command to display a count of users by role
- add a command to reset all users to default role (except for user with forbidden roles)
- add a commands to reset role on users with role from a group (except for user with forbidden roles)
- add commands to enforce or remove all prefixes (without changing roles)
- send message on nickname change
- randomly change its game status
- check regularly [RSS](https://www.rssboard.org/rss-specification) feeds and send messages with the links in a channel (can filter link with [regexp](https://en.wikipedia.org/wiki/Regular_expression) or translate an extract (call [DeepL API](https://www.deepl.com/)))
- monitor user activity (number of messages, last message date, last vocal interaction date) with regular save and a command to retrieve those data as a csv file (or save to a [Google Drive](https://drive.google.com/) folder)
- respond to message sended to it depending on keyword response rules (configured with a json file, can be changed by commands)
