---
description: INX-Participation is an extension that tracks on-tangle voting events.
image: /img/Banner/banner_hornet.png
keywords:
- IOTA Node
- Hornet Node
- INX
- Participation
- IOTA
- Shimmer
- Node Software
- Welcome
- explanation
---

# Welcome to INX-Participation

There is no dedicated structure for a voting event on the Tangle. Instead, it is represented by multiple individual transactions with specific payloads. To access the state of a voting event and the amount and distribution of the votes, you would have to find all these transactions in the Tangle and count each vote. INX-Participation does that for you: it builds a database of all voting transactions for given events and provides REST API endpoints for clients to access this information. Node operators manually select which events they wish to track.

You can find more information about participation events in the [Hornet Participation](https://github.com/iota-community/treasury/blob/main/specifications/hornet-participation-plugin.md) plugin specifications.

## Setup

The recommended setup is to use the provided [Docker images](https://hub.docker.com/r/iotaledger/inx-participation).
These images are also used in our [HORNET recommended setup using Docker](http://wiki.iota.org/hornet/develop/how_tos/using_docker).

## Configuration

The participation extension connects to the local Hornet instance by default.

You can find all the configuration options in the [configuration section](reference/configuration.md).

## Dashboard

If you are using the [INX-Dashboard](https://github.com/iotaledger/inx-dashboard) on your node, you can manage events directly from your browser.

## API

The extension exposes a custom set of REST APIs that can be used by wallets and applications to find past, active, and upcoming participation events and query event results.

You can find more information about the API in the [API reference section](reference/api_reference.md).

## Source Code

The source code of the project is available on [GitHub](https://github.com/iotaledger/inx-participation).