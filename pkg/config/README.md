# Configuration

This project supports an optional YAML config file at:

```text
<data-dir>/config.yaml
```

The server checks this file at startup if it exists. Validation problems are logged, but they do not stop the server from starting.

## Example

```yaml
sources:
  - id: github
    platform: github
    username: octocat
    token: $encrypted$1$REPLACE_WITH_ENCRYPTED_VALUE
    only_include_repos:
      - hello-world
    exclude_repos:
      - archived-repo
concurrency: 5
```

## Top-level fields

### `sources`

- Type: array of source objects
- Required: no, but having no configured sources produces a `warning`

Each source supports the following fields:

| Field                | Type     | Required | Notes                              |
| -------------------- | -------- | -------- | ---------------------------------- |
| `id`                 | string   | yes      | Must be unique across all sources  |
| `platform`           | string   | yes      | v1 only supports `github`          |
| `username`           | string   | yes      | Git provider username              |
| `token`              | string   | yes      | Must use `$encrypted$1$...` format |
| `only_include_repos` | string[] | no       | Optional allowlist                 |
| `exclude_repos`      | string[] | no       | Optional blocklist                 |

### `concurrency`

- Type: integer
- Required: no
- Default: `5`
- Validation: if explicitly set, it must be greater than `0`

## Validation

Validation issues have three severity levels:

- `error`: the config is invalid
- `warning`: the config is suspicious but still accepted
- `info`: reserved for future use

### Current `error` rules

- Invalid YAML syntax or decode failure
- Missing required source fields: `id`, `platform`, `username`, `token`
- Unsupported `platform` value
- Duplicate source `id`
- `concurrency <= 0`
- Source-specific check requested for a non-existent source ID
- `token` is plain text instead of `$encrypted$1$...`
- Encrypted `token` payload is malformed
- Encrypted `token` cannot be decrypted with the configured passphrase

### Current `warning` rules

- Unknown fields at the top level or inside a source
- `sources` is empty

## Check RPCs

Config checks are exposed through the Connect RPC service:

```text
gitplus.config.v1.ConfigService
```

The service is mounted under the `/api` base path.

### Check the whole config

- RPC: `CheckConfig`
- Generated client path: `/api/gitplus.config.v1.ConfigService/CheckConfig`
- Request message: `CheckConfigRequest`
- Response message: `CheckConfigResponse`

Response fields:

- `path`
- `exists`
- `issues`
- `summary`

Example response shape:

```json
{
  "path": "/path/to/data-dir/config.yaml",
  "exists": true,
  "issues": [
    {
      "severity": "SEVERITY_WARNING",
      "code": "unknown_field",
      "message": "field \"unexpected\" is not recognized",
      "path": "sources[0].unexpected",
      "line": 7
    }
  ],
  "summary": {
    "error": 0,
    "warning": 1,
    "info": 0
  }
}
```

### Check one source

- RPC: `CheckSourceConfig`
- Generated client path: `/api/gitplus.config.v1.ConfigService/CheckSourceConfig`
- Request message: `CheckSourceConfigRequest`
- Required request field: `source_id`
- Response message: `CheckSourceConfigResponse`

Response fields:

- `path`
- `exists`
- `source_id`
- `issues`
- `summary`

### Frontend usage

The frontend should call this service through the generated Connect client and the shared transport in [transport.ts](/Users/wangxuan/Documents/PProjects/git-plus/frontend/src/lib/connect/transport.ts).

Legacy REST-style paths such as `/api/config/check` and `/api/config/sources/{id}/check` are no longer supported.

## Token encryption

`token` values must not be stored as plain text. The config only accepts values in this format:

```text
$encrypted$1$<payload>
```

Set the passphrase with:

```text
ENCRYPT_PASSPHRASE
```

Generate an encrypted token with the CLI:

```bash
printf '%s' 'ghp_xxx' | ENCRYPT_PASSPHRASE='your passphrase' git-plus config encrypt-token
```

The command reads the plain token from standard input and prints an encrypted token that can be pasted into `config.yaml`.
The `git-plus` server command also requires `ENCRYPT_PASSPHRASE` to be present before startup begins; if it is missing, the Cobra command exits immediately.

### Token-related error codes

- `unencrypted_token`
- `invalid_encrypted_token`
- `token_decryption_failed`

## Notes

- The config file is optional in v1.
- The app currently only defines and validates this config. It does not yet use the config to drive business behavior.
- Advanced YAML features are not part of the supported contract. Keep the file to plain mappings, sequences, and scalar values.
