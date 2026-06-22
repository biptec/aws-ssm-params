# aws-ssm-params

`aws-ssm-params` is a CLI/TUI for discovering, filtering, reading, importing, exporting, and editing AWS SSM Parameter Store values.

The tool uses the AWS SDK for Go. AWS CLI is not required at runtime.

## Installation

Homebrew users can install the release from the Biptec tap:

```bash
brew tap biptec/tools https://github.com/biptec/homebrew-tools
brew install biptec/tools/aws-ssm-params
```

## Releases

Version tags matching `v*` are released by GitHub Actions with GoReleaser. The release workflow builds the GitHub Release artifacts and publishes the Homebrew Formula Ruby file to `biptec/homebrew-tools`.

For cross-repository Homebrew publication, configure a repository secret named `HOMEBREW_TAP_GITHUB_TOKEN`. The token must have contents write access to `biptec/homebrew-tools`; the default workflow `GITHUB_TOKEN` is only used for the source repository release and is not enough for writing to the tap repository.

## Commands

```text
aws-ssm-params [global options] <command> [command options]

Commands:
  tui      Open the TUI
  import   Import parameter values from stdin
  export   Export parameter values to stdout
```

## Global options

```text
--region value       AWS region; repeat the flag for multiple regions
--all-regions        Search parameters across all enabled AWS regions
--profile value      AWS profile
--no-color           Disable colored output
--keymap value       Keyboard navigation style: emacs or vi
--log-level value    Log level: trace, debug, info, warn, error, or off
--filters-file path  File with filter groups; one OR group per line
--filter value       Filter group; repeat for OR groups
```

Global options can be written before or after the command name:

```bash
aws-ssm-params --region eu-north-1 export
aws-ssm-params export --region eu-north-1
```

Logging is disabled by default (`--log-level=off`). Any enabled log level writes structured `log/slog` output to stderr, so stdout stays machine-readable and logs can be saved with normal shell redirection, for example `aws-ssm-params --log-level trace tui 2>debug.log`. SecureString values are always masked in logs. Trace logging enables low-level AWS HTTP timing diagnostics.

CLI list values are passed by repeating flags:

```bash
aws-ssm-params --region eu-north-1 --region eu-central-1 tui
```

Comma-separated lists are allowed only in environment variables:

```bash
AWS_SSM_PARAMS_REGIONS=eu-north-1,eu-central-1 aws-ssm-params tui
```

## Explicit parameter inventory

Pipe or redirect a newline-based inventory into `tui` when you already know which infrastructure parameters should exist. Stdin inventory is not a filter: it builds the desired TUI inventory. Names that are not found in AWS are still shown in the TUI as missing rows so they can be created.

```bash
cat path/to/names.txt | aws-ssm-params --region eu-north-1 tui

aws-ssm-params --region eu-north-1 tui < path/to/names.txt
```

The stdin inventory format is newline-based. Empty lines and `#` comments are ignored.

```text
/stage/prod/test
/stage/dev/test
```

`--filters-file` is separate: it filters loaded/discovered/imported parameters, while stdin inventory declares expected parameter names.

## Filters

Filters select parameters by field using extglob patterns.

```text
field:pattern
pattern              # shortcut for name:pattern
```

Supported filter fields:

```text
name
region
type
tier
data-type
description
policies
value
```

One `--filter` value is one OR group. Conditions inside one value are separated by semicolons and are combined with AND:

```bash
aws-ssm-params export \
  --filter 'name:/prod/*;region:eu-north*' \
  --filter 'name:/github/token;tier:advanced'
```

This means:

```text
(name:/prod/* && region:eu-north*) || (name:/github/token && tier:advanced)
```

A bare filter value is a name filter:

```bash
aws-ssm-params export --filter '/app/prod/**'
```

The same as:

```bash
aws-ssm-params export --filter 'name:/app/prod/**'
```

`--filters-file` uses the same syntax. One non-empty line is one OR group. `#` comments and blank lines are ignored.

```text
name:/prod/*;region:eu*;tier:advanced
/app-infra/production/himins/sparkyfitness
```

Extglob path matching uses these SSM path rules:

```text
*   matches one path segment and does not cross /
**  matches recursively and can cross /
```

Filters are global. For `import`, input data is parsed first, global filters are applied to the parsed records, and only then does the command query AWS or write parameters.

The AWS query pipeline uses `DescribeParameters` first for AWS-side prefiltering, then applies the exact extglob matcher locally. When values are needed, it enriches the selected parameters with `GetParameters` in batches.

## SecureString values

SecureString values are not decrypted or shown by default. Existing encrypted values are displayed as `(encrypted)` until `--with-decryption` is used. Use `--with-decryption` when values must be loaded, exported, edited, or matched through the `value` filter.

## tui

```bash
aws-ssm-params tui \
  --filter 'name:/prod/*;region:eu*' \
  --show-column name \
  --show-column value \
  --sort-by name:asc \
  --sort-by type:desc
```

Options:

```text
--with-decryption
--show-column value       repeat for multiple columns
--sort-by field:asc|field:desc  repeat for multiple sort columns
--no-confirm-overwrite-file
--no-confirm-write-securestring
--no-confirm-delete-one
--no-confirm-delete-all
```

Use repeated `--sort-by` flags or comma-separated `AWS_SSM_PARAMS_SORT_BY` values to sort by multiple columns in priority order.

## export

`export` always writes parameter data to stdout. Redirect stdout when you want a file.

```bash
aws-ssm-params export \
  --filter '/app-infra/prod/**' \
  --map-field name:title \
  --map-field value:text \
  --format json \
  --sort-by type:asc \
  --sort-by name:asc \
  --with-decryption
```

Options:

```text
--output-field aws_field
--map-field aws_field:file_field
--sort-by field:asc|field:desc  repeat for multiple sort columns
--with-decryption
--format dotenv|json|yaml
--key-field field
--scalar
```

Export sorting is applied before writing records to stdout. Use repeated `--sort-by` flags or comma-separated `AWS_SSM_PARAMS_SORT_BY` values to sort by multiple fields in priority order.

By default, JSON and YAML export write an array of records:

```json
[
  {
    "title": "/app/prod/token",
    "text": "secret"
  }
]
```

With `--key-field name`, JSON export writes an object and YAML export writes a map keyed by the selected AWS field:

```json
{
  "/app/prod/token": {
    "text": "secret"
  }
}
```

## import

`import` always reads parameter data from stdin. Redirect or pipe data into the command.

```bash
cat .env | aws-ssm-params import \
  --format dotenv \
  --root-path /app-infra/prod/biptec/website \
  --default-type SecureString
```

`--root-path` prefixes relative names from dotenv, JSON, and YAML inputs. Absolute SSM names and explicit `# ssm: /path` dotenv comments are kept unchanged.

```bash
cat values.yaml | aws-ssm-params import \
  --format yaml \
  --map-field name:title \
  --map-field value:text \
  --key-field name \
  --default-type SecureString
```

Options:

```text
--map-field aws_field:file_field
--format dotenv|json|yaml
--key-field field
--root-path path
--on-create skip|error|ask
--on-update skip|error|ask
--continue-on-error
--summary
--default-type value
--default-tier value
--default-data-type value
--default-region value
--default-description value
--default-policies value
--default-policies-file path
```

By default, import uses upsert semantics: missing parameters are created and existing parameters are updated. `--on-create` and `--on-update` only change this default when they are set:

```text
skip   log and skip the operation
error  log and stop at the operation
ask    ask whether to perform the operation
```

`--continue-on-error` logs a per-record error, skips that record, and continues with the next one. If any records failed, the command exits with status `1` after processing. `--summary` prints final created/updated/skipped/failed counts; no summary is printed by default.

For existing parameters, import resolves metadata fields in this order:

```text
imported file value → cloud metadata → default flag/built-in default
```

For new parameters, the order is:

```text
imported file value → default flag → built-in default
```

`value` is required in the input data and is never read from AWS. This avoids reading or decrypting existing SecureString values during import.

AWS field names follow SSM API terminology:

```text
type       String, StringList, SecureString
tier       Standard, Advanced, Intelligent-Tiering
data-type  text, aws:ec2:image, aws:ssm:integration
```

## Single-field scalar export

Use `--scalar` with exactly one `--output-field` to print one selected field per matching parameter.

```bash
aws-ssm-params export --output-field name --scalar

aws-ssm-params export \
  --filter 'name:/app/prod/token' \
  --output-field value \
  --scalar \
  --with-decryption
```

Default/dotenv format writes one scalar value per line. JSON writes an array of scalar values, and YAML writes a list:

```bash
aws-ssm-params export --format json --output-field name --scalar
```

```json
[
  "/app/a",
  "/app/b"
]
```

With `--key-field`, JSON/YAML scalar export writes a map from key field to scalar value:

```bash
aws-ssm-params export \
  --format json \
  --key-field name \
  --output-field value \
  --scalar \
  --with-decryption
```

```json
{
  "/app/a": "secret-a",
  "/app/b": "secret-b"
}
```

`--scalar` requires exactly one `--output-field`. Use `--map-field` only for object/map output where field names are present.
