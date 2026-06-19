# aws-ssm-params

`aws-ssm-params` is a CLI/TUI for discovering, filtering, reading, importing, exporting, and editing AWS SSM Parameter Store values.

The tool uses the AWS SDK for Go. AWS CLI is not required at runtime.

## Commands

```text
aws-ssm-params [global options] <command> [command options]

Commands:
  interactive, tui  Open the interactive TUI
  get               Print one selected parameter field
  put               Put a String, StringList, or SecureString parameter value
  import            Import parameter values from stdin
  export            Export parameter values to stdout
```

## Global options

```text
--region value       AWS region; repeat the flag for multiple regions
--name value         Explicit SSM parameter name to include; repeat for multiple names
--names-file path    File with explicit SSM parameter names to include
--all-regions        Search parameters across all enabled AWS regions
--profile value      AWS profile
--no-color           Disable colored output
--keymap value       Keyboard navigation style: emacs or vi
--log-level value    Log level: debug, info, warn, error, or off
--log-target value   Log target: stderr, stdout, or file
--log-file path      Log file path used when --log-target=file
```

Logging is disabled by default (`--log-level=off`). API errors and requests are logged with structured `log/slog` output when enabled. SecureString values are always masked in logs. During TUI sessions, stdout/stderr logs are buffered and printed after the TUI exits; `--log-target=file` writes directly to `--log-file`.

CLI list values are passed by repeating flags:

```bash
aws-ssm-params --region eu-north-1 --region eu-central-1 --name /stage/prod/test interactive
```

Comma-separated lists are allowed only in environment variables:

```bash
AWS_SSM_PARAMS_REGIONS=eu-north-1,eu-central-1 AWS_SSM_PARAMS_NAME=/stage/prod/test,/stage/dev/test aws-ssm-params interactive
```


## Explicit parameter inventory

Use `--name` or `--names-file` when you already know which infrastructure parameters should exist. These options are not filters: they build the desired inventory. Names that are not found in AWS are still shown in the TUI as missing rows so they can be created.

```bash
aws-ssm-params \
  --region eu-north-1 \
  --name /stage/prod/test \
  --name /stage/dev/test \
  interactive

aws-ssm-params \
  --region eu-north-1 \
  --names-file path/to/names.txt \
  tui
```

`--names-file` is newline-based. Empty lines and `#` comments are ignored.

```text
/stage/prod/test
/stage/dev/test
```

Environment variables use comma-separated values for repeated flags:

```bash
AWS_SSM_PARAMS_NAME=/stage/prod/test,/stage/dev/test
AWS_SSM_PARAMS_NAMES_FILE=path/to/names.txt
```

`--filters-file` is separate: it filters loaded/discovered parameters, while `--names-file` declares expected parameter names.

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

The AWS query pipeline uses `DescribeParameters` first for AWS-side prefiltering, then applies the exact extglob matcher locally. When values are needed, it enriches the selected parameters with `GetParameters` in batches.

## SecureString values

SecureString values are not decrypted or shown by default. Existing encrypted values are displayed as `(encrypted)` until `--with-decryption` is used. Use `--with-decryption` when values must be loaded, exported, edited, or matched through the `value` filter.

## interactive

```bash
aws-ssm-params interactive \
  --names-file path/to/names.txt \
  --filter 'name:/prod/*;region:eu*' \
  --show-column name \
  --show-column value \
  --sort-column name:asc
```

Options:

```text
--filters-file path
--filter filter-group
--with-decryption
--show-column value       repeat for multiple columns
--sort-column field:asc|field:desc
--no-confirm-overwrite-file
--no-confirm-write-securestring
--no-confirm-delete-one
--no-confirm-delete-all
```

Only one `--sort-column` value is supported.

## export

`export` always writes parameter data to stdout. Redirect stdout when you want a file.

```bash
aws-ssm-params export \
  --filter '/app-infra/prod/**' \
  --field name:title \
  --field value:text \
  --format json \
  --with-decryption
```

Options:

```text
--filter filter-group
--field aws_field[:file_field]
--with-decryption
--format dotenv|json
--json-key-field field
--include-missing
```

By default, JSON export writes an array of records:

```json
[
  {
    "title": "/app/prod/token",
    "text": "secret"
  }
]
```

With `--json-key-field name`, JSON export writes an object keyed by the selected AWS field:

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
cat values.json | aws-ssm-params import \
  --format json \
  --field name:title \
  --field value:text \
  --json-key-field name \
  --default-type SecureString
```

Options:

```text
--filters-file path
--filter filter-group
--field aws_field[:file_field]
--format dotenv|json
--json-key-field field
--default-value value
--default-value-file path
--default-override
--default-type value
--default-tier value
--default-data-type value
--default-region value
--default-description value
--default-policies value
--default-policies-file path
```

AWS field names follow SSM API terminology:

```text
type       String, StringList, SecureString
tier       Standard, Advanced, Intelligent-Tiering
data-type  text, aws:ec2:image, aws:ssm:integration
```

## put

```bash
aws-ssm-params put /app/prod/token secret \
  --type SecureString \
  --tier Advanced \
  --region eu-north-1
```

Options:

```text
--override value
--type value
--tier value
--data-type value
--region value
--description value
--policies value
--policies-file path
```

## get

```bash
aws-ssm-params get /app/prod/token value --with-decryption
aws-ssm-params get /app/prod/token type
aws-ssm-params get /app/prod/token name
```

Allowed fields:

```text
name
value
type
tier
data-type
region
description
policies
```
