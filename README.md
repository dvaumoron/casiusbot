# CasiusBot

A [Discord](https://discord.com/) bot, with the following features :
- add a prefix on nickname based on the user's roles (with priority to some "special" roles).
- add a default role to user with any prefix role
- check regularly [RSS](https://www.rssboard.org/rss-specification) feeds and send messages with the links in a channel.
- post reminder messages for scheduled events

Optionally (when corresponding configuration is present) :
- remove a member reaction on a given message (supposed to be the only message pinned in the specified channel) on guild member remove (in order to clean role counter when using [reaction roles](https://docs.carl.gg/#/roles?id=reaction-roles))
