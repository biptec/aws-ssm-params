# aws-ssm-params

A fast, terminal-first CLI/TUI for managing **AWS Systems Manager Parameter Store** values.

`aws-ssm-params` helps developers, DevOps engineers, and platform teams inspect, edit, import, export, and audit `String`, `StringList`, and `SecureString` parameters without building one-off scripts or clicking through the AWS Console.

It can work as a full SSM parameter browser by discovering parameters directly from AWS, or it can use an optional paths file as a focused filter for a known set of expected parameters.

## Why use it?

AWS SSM Parameter Store is simple and reliable, but day-to-day secret maintenance can become annoying:

- Which required parameters are missing before a deployment?
- Which values exist in one region but not another?
- Which parameters are empty, outdated, or accidentally duplicated?
- How can I rotate a secret without exposing every value on screen?
- How can I export/import a known set of parameters in a repeatable format?
- How can I onboard a new environment without manually opening dozens of AWS Console pages?

`aws-ssm-params` gives you one terminal tool for those jobs.

## Features

- Interactive Bubble Tea TUI for browsing and editing SSM parameters.
- Works with all standard SSM parameter types: `SecureString`, `String`, and `StringList`.
- Can discover SSM parameters directly from AWS, or read an optional paths file with one SSM path per line.
- Single-region, selected multi-region, and all-enabled-regions scanning.
- Keeps secret values hidden by default in the TUI.
- Optional value reveal, SHA-256 prefix, version, tier, user, description, and date columns populated from AWS SSM metadata/runtime state.
- Search/filter mode for large parameter lists.
- Edit existing values or create new parameters inline, while preserving or choosing the SSM parameter type.
- Generate random secrets: base64, hex, or UUID.
- Import/export as dotenv or JSON, including optional parameter type metadata.
- Refuses to overwrite existing non-empty values unless `--override` is used.
- Uses the local AWS CLI, so it works with your existing AWS profiles, SSO sessions, and credential setup.

## Typical use cases

### Deployment readiness checks

Keep a `paths.txt` file beside your application or infrastructure code and run the TUI before deploying:

```bash
aws-ssm-params --region eu-north-1 --paths-file paths.txt
```

You immediately see which required parameters are present, missing, empty, or failing due to AWS permission/API errors.

### New environment onboarding

Create a list of expected paths for a new environment, open the TUI, fill missing values, and save them as encrypted `SecureString` parameters by default, or choose `String` / `StringList` when plaintext parameters are intentional.

```text
/my-product/prod/api/DATABASE_URL
/my-product/prod/api/JWT_SECRET
/my-product/prod/api/STRIPE_SECRET_KEY
/my-product/prod/web/NEXT_PUBLIC_API_URL
```

### Secret rotation

Open the existing secret, generate a new random value, preview it, and save it back to SSM.

The TUI can generate:

```text
base64-32
hex-32
uuid
custom base64 byte length
```

### Multi-region audits

Scan a known set of parameters across selected regions:

```bash
aws-ssm-params --region eu-north-1 --region eu-central-1 --paths-file paths.txt
```

Or scan all AWS regions enabled for the account:

```bash
aws-ssm-params --all-regions --paths-file paths.txt
```

This is useful when you need to verify whether replicated apps, disaster-recovery regions, or regional workloads have the same required secret set.

### Backup, migration, and review

Export a known set of parameters:

```bash
aws-ssm-params --region eu-north-1 --paths-file paths.txt export --file values.env
```

Import them into another account/profile/region:

```bash
AWS_PROFILE=target aws-ssm-params --region eu-central-1 --paths-file paths.txt import --file values.env
```

> Exported files contain plaintext secrets. Store them carefully, encrypt them at rest, and delete them when they are no longer needed.

## Requirements

- Go 1.23+ to build from source.
- AWS CLI installed and available as `aws` in `PATH`.
- AWS credentials configured through environment variables, named profiles, SSO, instance role, or any other AWS CLI-supported method.
- IAM permissions for the operations you plan to use.

For read-only browsing and export, you typically need:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ssm:DescribeParameters",
        "ssm:GetParameters"
      ],
      "Resource": "*"
    }
  ]
}
```

For editing/importing/deleting values, also allow:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ssm:PutParameter",
        "ssm:DeleteParameters"
      ],
      "Resource": "*"
    }
  ]
}
```

For `--all-regions`, also allow:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeRegions"
      ],
      "Resource": "*"
    }
  ]
}
```

Tighten `Resource` ARNs to your own path prefix when using this in production.

## Installation

### Install with Go

```bash
go install github.com/biptec/aws-ssm-params/cmd/aws-ssm-params@latest
```

Make sure your Go bin directory is in `PATH`:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Build from source

```bash
git clone https://github.com/biptec/aws-ssm-params.git
cd aws-ssm-params
go build -o aws-ssm-params ./cmd/aws-ssm-params
```

Then run:

```bash
./aws-ssm-params --help
```

## Quick start

Create a paths file:

```bash
cat > paths.txt <<'EOF_PATHS'
/my-app/dev/api/DATABASE_URL
/my-app/dev/api/JWT_SECRET
/my-app/dev/api/STRIPE_SECRET_KEY
/my-app/dev/web/SESSION_SECRET
EOF_PATHS
```

Open the interactive TUI:

```bash
aws-ssm-params --profile dev --region eu-north-1 --paths-file paths.txt
```

Inside the TUI:

- Press `ctrl+/` to open the context-sensitive `Shortcuts` page.
- Press `d` on the main screen to show/hide full details for the selected parameter.
- Press `Enter` on the main screen to edit a value, or `n` to create a new parameter.
- Press `ctrl+r` inside the editor to generate a random value into `Value`; review it, then press `ctrl+s` to save.
- Press `/` on the main screen to search.
- Press `esc` to go back from inner screens; on the main screen, `esc` quits.

## Paths file format

The paths file is intentionally simple and tool-agnostic:

```text
# Comments are ignored
/my-app/dev/api/DATABASE_URL
/my-app/dev/api/JWT_SECRET

# Inline comments are also supported
/my-app/dev/api/STRIPE_SECRET_KEY # used by payments service
```

Rules:

- One SSM path per line.
- Empty lines are ignored.
- Full-line comments starting with `#` are ignored.
- Inline comments after a path are stripped.
- Paths must start with `/`.
- Duplicate paths are ignored.
- Paths are sorted before display/export.

This makes it easy to keep parameter requirements in Git without storing any secret values.

## Command overview

```text
aws-ssm-params [global options] <command> [command options]
aws-ssm-params [global options] get <path> [--file path]
aws-ssm-params [global options] set <path> <value> [--type string|string-list|secure-string] [--override]
aws-ssm-params [global options] set <path> --file path [--type string|string-list|secure-string] [--override]
aws-ssm-params [global options] import [--format dotenv] [--file path] [--type string|string-list|secure-string] [--override]
aws-ssm-params [global options] export [--format dotenv|json] [--file path] [--include-missing]
```

Opening the TUI is the default behavior. Without `--paths-file`, the TUI discovers parameters directly from AWS for the selected region(s):

```bash
aws-ssm-params --region eu-north-1
```

Use `--paths-file` when you want to filter the TUI to a known set of paths:

```bash
aws-ssm-params --region eu-north-1 --paths-file paths.txt
```

By default, `--paths-file` is treated as a read-only filter/list. New parameters created in the TUI appear immediately in the current UI, but the file is not changed. Add `--allow-paths-file-update` when you want the TUI to append newly created paths and remove deleted paths from that file.

## Global options

```text
--region REGION      AWS region. Repeat or comma-separate to scan selected regions.
--all-regions        Search parameters across all enabled AWS regions.
--profile PROFILE    AWS profile name.
--no-color           Disable colored output.
--keymap KEYMAP      Keyboard navigation style: emacs or vi (default: emacs).
--paths-file FILE    Optional file with SSM parameter paths to load/filter.
--columns LIST       Comma-separated optional TUI columns to show on startup.
--allow-paths-file-update
                     Allow the TUI to update --paths-file on create/delete.
```

All global options can also be configured through environment variables:

```text
AWS_SSM_PARAMS_REGION
AWS_SSM_PARAMS_ALL_REGIONS
AWS_SSM_PARAMS_PROFILE
AWS_SSM_PARAMS_PATHS_FILE
AWS_SSM_PARAMS_NO_COLOR
AWS_SSM_PARAMS_KEYMAP
AWS_SSM_PARAMS_COLUMNS
AWS_SSM_PARAMS_ALLOW_PATHS_FILE_UPDATE
```

Notes:

- `--region` and `--all-regions` cannot be used together.
- If `--region` is omitted, the tool falls back to `AWS_SSM_PARAMS_REGION`, `AWS_REGION`, `AWS_DEFAULT_REGION`, or AWS CLI profile configuration.
- `AWS_SSM_PARAMS_REGION` accepts one region or multiple comma-separated regions.
- Direct `get`, `set`, and `import` operate on one concrete region.
- `interactive` and `export` support repeated/comma-separated `--region` and `--all-regions`.
- `--allow-paths-file-update` requires `--paths-file`.
- CLI flags override `AWS_SSM_PARAMS_*` environment variables.
- `--keymap emacs` uses Emacs-style navigation shortcuts in the TUI. `--keymap vi` uses vi-style navigation on list/selector screens and a modal `NORMAL`/`INSERT` editor for text fields.

## Parameter types

AWS SSM supports three standard parameter types, and `aws-ssm-params` can read, write, import, and export all of them:

```text
SecureString   Encrypted value; default for new parameters and recommended for secrets.
String         Plain text scalar value; useful for non-sensitive configuration.
StringList     Plain text comma-separated list; useful for allowlists or simple lists.
```

Type selection rules are intentionally conservative:

- Existing parameters preserve their current AWS type when you edit, set, or import without an explicit type.
- New/missing parameters are created as `SecureString` by default.
- `set --type` overrides the target type for that write.
- `import --type` sets the default type for imported records that do not include type metadata.
- Dotenv `# type:` comments and typed JSON records take priority during import.

Supported CLI aliases are `secure-string`, `string`, and `string-list`.

## Interactive TUI

The TUI is built for fast keyboard-driven maintenance.

The main screen starts as a single full-height list:

```text
List of N Parameters
```

Press `d` to show `Selected Parameter` above the list. It opens directly in full-detail mode and stays visible while you move through the parameter list until you press `d` again.

`List of N Parameters` shows all discovered or filtered paths with optional metadata columns. Press `n` to create a new parameter; the editor opens with focus on `SSM path`. With `--paths-file`, newly created paths are shown immediately in the UI. The file itself is updated only when `--allow-paths-file-update` is enabled.

### Managed paths file updates

When `--paths-file` is used without additional flags, it is read-only:

```text
create -> parameter appears in the current UI, paths-file unchanged
delete -> parameter is deleted in AWS and remains as a missing row, paths-file unchanged
```

Use `--allow-paths-file-update` to let the TUI keep the paths file in sync with create/delete operations:

```bash
aws-ssm-params --paths-file paths.txt --allow-paths-file-update
```

Then:

```text
create -> append path to paths-file if missing
delete -> remove path from paths-file and remove the row from the UI
```

Without `--paths-file`, deleted rows disappear from the UI because the list is discovered directly from AWS.

### Footer shortcuts

The footer only shows action shortcuts. Navigation shortcuts are available in the context-sensitive `Shortcuts` popup opened with `ctrl+/`. The `ctrl+/ help` shortcut is always shown first so it remains visible in narrow terminals. When a popup is open, the footer switches to the top popup actions.

Main footer when details are hidden:

```text
ctrl+/ help • enter edit • n new • d show details • / search • c columns • x delete • X delete visible • esc quit
```

Main footer when details are shown:

```text
ctrl+/ help • enter edit • n new • d hide details • / search • c columns • x delete • X delete visible • esc quit
```

Editor footer in Emacs keymap:

```text
ctrl+/ help • ctrl+s save • ctrl+r random • ctrl+o read file • ctrl+w write file • ctrl+k clear • esc back
```

Editor footer in Vi keymap, normal mode:

```text
ctrl+/ help • i insert • ctrl+s save • ctrl+r random • ctrl+o read file • ctrl+w write file • ctrl+k clear • esc back
```

Editor footer in Vi keymap, insert mode:

```text
ctrl+/ help • ctrl+s save • ctrl+r random • ctrl+o read file • ctrl+w write file • ctrl+k clear • esc normal
```

`ctrl+r random` is intentionally available in the editor, not on the main screen. It inserts the generated value into `Value`; the value is saved to AWS only after you press `ctrl+s`. `ctrl+o read file` reads the configured file path into `Value`.

### Shortcuts popup

Press `ctrl+/` to open the `Shortcuts` popup. It shows actions and navigation for the page or popup you opened it from. The navigation section follows the selected keymap. If `Shortcuts` is opened while another popup is active, it is stacked above that popup; closing `Shortcuts` returns to the previous popup.

Emacs-style main/list navigation:

```text
↑ / ctrl+p / shift+tab     previous row/option
↓ / ctrl+n / tab           next row/option
PgUp / alt+v               page up
PgDn / ctrl+v              page down
Home / alt+<               first row/option
End / alt+>                last row/option
```

Vi-style main/list navigation:

```text
↑ / k / shift+tab          previous row/option
↓ / j / tab                next row/option
PgUp                       page up
PgDn                       page down
Home / gg                  first row/option
End / G                    last row/option
```

Editor navigation in `--keymap vi` is modal. The editor opens in `NORMAL` mode, `i` enters `INSERT` mode, `esc` leaves `INSERT` mode, and a second `esc` goes back. While inserting, the active editable field label shows `[INSERT]`, for example `SSM path [INSERT]:`, `File path [INSERT]:`, or `Value [INSERT]:`.

Vi editor shortcuts include:

```text
h / l                      backward/forward character
j / k                      next/previous line in Value
w / b                      forward/backward word
0 / $                      start/end of line
gg / G                     start/end of text
x                          delete current character
dw / db                    delete next/previous word
```

Hidden duplicate back/quit shortcuts such as `q` and `ctrl+g` remain available and are documented in `Shortcuts`, but the footer shows `esc` as the primary back/quit key.

### Columns

The `#` and `PATH` columns are always visible. Use `--columns` to choose startup columns, or press `c` in the TUI to open the Columns popup over the current main screen. The popup lets you choose optional columns populated from AWS SSM metadata/runtime state:

```text
REGION
DATE
TYPE
TIER
VERSION
LEN
SHA256
VALUE
USER
DESCRIPTION
```

Examples:

```bash
aws-ssm-params --columns region,type,value
AWS_SSM_PARAMS_COLUMNS=region,type,value aws-ssm-params
```

Column widths are calculated dynamically from the current result set and terminal width.

## Import and export

Supported formats:

```text
dotenv
json
```

The default format is `dotenv`.

### Dotenv export

```bash
aws-ssm-params --region eu-north-1 --paths-file paths.txt export --file values.env
```

Example output:

```dotenv
# ssm: /my-app/dev/api/DATABASE_URL
# type: String
DATABASE_URL="postgres://user:pass@example.com:5432/app"

# ssm: /my-app/dev/api/JWT_SECRET
# type: SecureString
JWT_SECRET="super-secret-value"
```

The `# ssm:` comment is the canonical SSM path. The optional `# type:` comment preserves the AWS SSM parameter type. The variable name is a readable alias.

During import, the `# ssm:` comment takes priority. If the comment is missing, the tool tries to resolve the variable name from the paths file. Ambiguous aliases are rejected.

### JSON export

```bash
aws-ssm-params --region eu-north-1 --paths-file paths.txt export --format json --file values.json
```

Example output:

```json
{
  "/my-app/dev/api/DATABASE_URL": {
    "type": "String",
    "value": "postgres://user:pass@example.com:5432/app"
  },
  "/my-app/dev/api/JWT_SECRET": {
    "type": "SecureString",
    "value": "super-secret-value"
  }
}
```

JSON uses SSM paths as keys, so it can be imported without alias resolution. The compact legacy JSON shape `{ "/path": "value" }` is still accepted for imports; typed exports use `{ "type": "...", "value": "..." }` records when type metadata is available.

### Import

Dotenv import requires `paths.txt` because dotenv keys are aliases and the tool needs the paths file to resolve them back to full SSM paths:

```bash
aws-ssm-params --region eu-north-1 --paths-file paths.txt import --file values.env
```

JSON import does not require `paths.txt`, because JSON uses full SSM paths as keys:

```bash
aws-ssm-params --region eu-north-1 import --format json --file values.json
```

You may still pass `--paths-file paths.txt` with JSON if you want to keep imports scoped to a known path set, but it is optional for that format:

```bash
aws-ssm-params --region eu-north-1 --paths-file paths.txt import --format json --file values.json
```

By default, import will not overwrite existing non-empty values. Imported records with type metadata keep that type. Records without metadata preserve the existing AWS type, or use `SecureString` for new parameters. You can set a different default type:

```bash
aws-ssm-params --region eu-north-1 --paths-file paths.txt import --file app.env --type string
```

For JSON without `paths.txt`:

```bash
aws-ssm-params --region eu-north-1 import --format json --file values.json --type string
```

To overwrite existing non-empty values intentionally:

```bash
aws-ssm-params --region eu-north-1 --paths-file paths.txt import --file values.env --override
aws-ssm-params --region eu-north-1 import --format json --file values.json --override
```

## Direct get/set

Read one parameter:

```bash
aws-ssm-params --region eu-north-1 get /my-app/dev/api/JWT_SECRET
```

Write the value to a file instead of stdout:

```bash
aws-ssm-params --region eu-north-1 get /my-app/dev/api/TLS_KEY --file tls.key
```

Set one value. Existing parameters keep their current AWS type when `--type` is omitted; new parameters are created as `SecureString` by default:

```bash
aws-ssm-params --region eu-north-1 set /my-app/dev/api/JWT_SECRET "new-secret-value"
```

Set a plaintext configuration value intentionally:

```bash
aws-ssm-params --region eu-north-1 set /my-app/dev/api/LOG_LEVEL debug --type string
```

Set a comma-separated StringList value:

```bash
aws-ssm-params --region eu-north-1 set /my-app/dev/api/ALLOWED_HOSTS "api.example.com,www.example.com" --type string-list
```

Set one value from a file:

```bash
aws-ssm-params --region eu-north-1 set /my-app/dev/api/TLS_KEY --file tls.key
```

Overwrite an existing non-empty value:

```bash
aws-ssm-params --region eu-north-1 set /my-app/dev/api/JWT_SECRET "new-secret-value" --override
```

All writes use:

```text
Type: existing type, explicit --type, imported type metadata, or SecureString by default
Tier: Intelligent-Tiering
Overwrite: true after protection checks pass
```

## Region modes

### Single region

```bash
aws-ssm-params --region eu-north-1
```

All visible SSM parameters are discovered in `eu-north-1`. Add `--paths-file paths.txt` to check only the paths listed in that file.

### Selected regions

```bash
aws-ssm-params --region eu-north-1 --region eu-central-1
```

Parameters are discovered in each selected region. With `--paths-file paths.txt`, each listed path is searched in the selected regions; existing parameters are shown as regional rows and paths missing from every scanned region are shown as wildcard missing rows.

### All enabled regions

```bash
aws-ssm-params --all-regions
```

The tool calls `ec2:DescribeRegions`, filters out not-opted-in regions, and scans the remaining enabled regions. Add `--paths-file paths.txt` to filter the scan to a known set of paths.

## Security notes

- Values are hidden by default in the TUI.
- Pressing `v` reveals cached values on screen; it is documented on `Shortcuts` instead of the footer. Be careful when screen sharing or recording.
- Exported dotenv/JSON files contain plaintext secrets.
- Use restrictive file permissions and encrypted storage for exported files.
- Prefer short-lived AWS credentials or SSO sessions.
- Scope IAM policies to your SSM path prefix when possible.
- Review `--override` and delete operations carefully.

## Development

Run tests:

```bash
go test ./...
```

Run coverage:

```bash
go test -cover ./...
```

Build:

```bash
go build ./cmd/aws-ssm-params
```

## Project status

`aws-ssm-params` focuses on one job: making AWS SSM Parameter Store maintenance fast, visible, and repeatable from the terminal.

It is intentionally small, script-friendly, and designed to fit into existing AWS CLI workflows.
