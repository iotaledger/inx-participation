---
description: This section describes the configuration parameters and their types for INX-Participation.
keywords:
- IOTA Node 
- Hornet Node
- Dashboard
- Configuration
- JSON
- Customize
- Config
- reference
---


# Core Configuration

INX-Participation uses a JSON standard format as a config file. If you are unsure about JSON syntax, you can find more information in the [official JSON specs](https://www.json.org).

You can change the path of the config file by using the `-c` or `--config` argument while executing `inx-participation` executable.

For example:
```bash
inx-participation -c config_defaults.json
```

You can always get the most up-to-date description of the config parameters by running:

```bash
inx-participation -h --full
```

## <a id="app"></a> 1. Application

| Name            | Description                                                                                            | Type    | Default value |
| --------------- | ------------------------------------------------------------------------------------------------------ | ------- | ------------- |
| checkForUpdates | Whether to check for updates of the application or not                                                 | boolean | true          |
| stopGracePeriod | The maximum time to wait for background processes to finish during shutdown before terminating the app | string  | "5m"          |

Example:

```json
  {
    "app": {
      "checkForUpdates": true,
      "stopGracePeriod": "5m"
    }
  }
```

## <a id="inx"></a> 2. INX

| Name    | Description                            | Type   | Default value    |
| ------- | -------------------------------------- | ------ | ---------------- |
| address | The INX address to which to connect to | string | "localhost:9029" |

Example:

```json
  {
    "inx": {
      "address": "localhost:9029"
    }
  }
```

## <a id="participation"></a> 3. Participation

| Name                    | Description                                                 | Type   | Default value    |
| ----------------------- | ----------------------------------------------------------- | ------ | ---------------- |
| [db](#participation_db) | Configuration for Database                                  | object |                  |
| bindAddress             | Bind address on which the Participation HTTP server listens | string | "localhost:9892" |

### <a id="participation_db"></a> Database

| Name   | Description                                     | Type   | Default value |
| ------ | ----------------------------------------------- | ------ | ------------- |
| engine | The used database engine (pebble/rocksdb/mapdb) | string | "rocksdb"     |
| path   | The path to the database folder                 | string | "database"    |

Example:

```json
  {
    "participation": {
      "db": {
        "engine": "rocksdb",
        "path": "database"
      },
      "bindAddress": "localhost:9892"
    }
  }
```

## <a id="profiling"></a> 4. Profiling

| Name        | Description                                       | Type    | Default value    |
| ----------- | ------------------------------------------------- | ------- | ---------------- |
| enabled     | Whether the profiling plugin is enabled           | boolean | false            |
| bindAddress | The bind address on which the profiler listens on | string  | "localhost:6060" |

Example:

```json
  {
    "profiling": {
      "enabled": false,
      "bindAddress": "localhost:6060"
    }
  }
```

