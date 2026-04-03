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
    token: secret
    only_include_repos:
      - hello-world
    exclude_repos:
      - archived-repo
concurrency: 5
```

## Top-level fields

### `sources`

- Type: array of source objects
- Required: no, but an empty list produces a `warning`

Each source supports the following fields:

| Field                | Type     | Required | Notes                             |
| -------------------- | -------- | -------- | --------------------------------- |
| `id`                 | string   | yes      | Must be unique across all sources |
| `platform`           | string   | yes      | v1 only supports `github`         |
| `username`           | string   | yes      | Git provider username             |
| `token`              | string   | yes      | Stored as plain text in v1        |
| `only_include_repos` | string[] | no       | Optional allowlist                |
| `exclude_repos`      | string[] | no       | Optional blocklist                |

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

### Current `warning` rules

- Unknown fields at the top level or inside a source
- `sources` is present but empty

## Check APIs

### Check the whole config

```http
GET /api/config/check
```

Response shape:

```json
{
  "path": "/path/to/data-dir/config.yaml",
  "exists": true,
  "target": "config",
  "issues": [
    {
      "severity": "warning",
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

```http
GET /api/config/sources/{id}/check
```

Example:

```http
GET /api/config/sources/github/check
```

The response format is the same, with:

- `target: "source"`
- `source_id` set to the requested source ID

## Notes

- The config file is optional in v1.
- The app currently only defines and validates this config. It does not yet use the config to drive business behavior.
- Advanced YAML features are not part of the supported contract. Keep the file to plain mappings, sequences, and scalar values.
