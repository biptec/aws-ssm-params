# aws-ssm-params

A fast, terminal-first CLI/TUI for managing **AWS Systems Manager Parameter Store**.

`aws-ssm-params` helps developers, DevOps engineers, and platform teams inspect, edit, import, export, and audit `String`, `StringList`, and `SecureString` parameters without writing one-off scripts or clicking through the AWS Console. Run the TUI without a names filter to browse everything in an account/region, or add `--names` / `--names-file` when you want to focus on a known application-specific set.

Use it when you want a quick answer to questions like: _what parameters exist in this account, what is missing before deployment, what differs between regions, which secrets are empty, which old parameters can be deleted, and how do I safely update or migrate a known set of parameters?_

## Contents

- [Why use it?](#why-use-it)
- [Features](#features)
- [Typical workflows](#typical-workflows)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Names file format](#names-file-format)
- [Command map](#command-map)
- [Global scope flags](#global-scope-flags)
- [Interactive TUI](#interactive-tui)
- [Import and export](#import-and-export)
- [Direct get/set](#direct-getset)
- [Region modes](#region-modes)
- [Security notes](#security-notes)
- [Development](#development)

## Why use it?

AWS SSM Parameter Store is reliable, but daily maintenance becomes slow when you need to compare environments, check missing values, rotate secrets, or copy parameters between accounts and regions.

`aws-ssm-params` gives you one terminal workflow for those jobs:

- Browse all visible Parameter Store names in a keyboard-driven TUI, even without a names file.
- Find old, unused, duplicate, or forgotten parameters that accumulated over time.
- Keep secret values hidden by default.
- Check a known application-specific parameter list from a names file.
- Compare one name set across one region, selected regions, or all enabled regions.
- Edit existing parameters or create missing ones without leaving the terminal.
- Generate random secrets and load/write values from files.
- Export/import dotenv or JSON for backup, migration, review, and onboarding.
- Use your existing AWS SDK-compatible profiles, SSO sessions, environment credentials, or instance role.

## Features

- Interactive Bubble Tea TUI for browsing, filtering, sorting, editing, creating, and deleting SSM parameters.
- Explicit CLI commands: `interactive`, `tui`, `export`, `import`, `get`, and `set`.
- Supports `SecureString`, `String`, and `StringList` parameters.
- Works with direct AWS discovery, `--names`, or a `--names-file`; no names filter means “show everything AWS returns”.
- Global `--fields` scope to load/show/import/export only the fields you need.
- Single-region, selected multi-region, and all-enabled-regions scanning.
- Secure values are hidden by default, with optional explicit reveal.
- Optional columns for value, type, region, date, version, tier, length, SHA-256 prefix, user, and description.
- Natural sorting, dynamic table widths, context-sensitive help, Emacs/Vi keymaps, and file-action confirmations.
- Advanced-parameter support for tier, data type, description, and parameter policies.
- Dotenv and JSON import/export, including type metadata where the format supports it.

## Typical workflows

### Parameter inventory and cleanup

Open the TUI without `--names` or `--names-file` to discover all visible parameters in the selected region. This is useful when you want to audit an AWS account, understand what already exists, find old leftovers, or delete parameters that are no longer used.

```bash
aws-ssm-params --regions eu-north-1 tui
```

Add `--fields name,type,date,user` or `--show-columns type,date,user` when you want a lightweight inventory view without loading values.

### Deployment readiness check

Keep a names file beside your service or infrastructure code:

```text
/my-product/prod/api/DATABASE_URL
/my-product/prod/api/JWT_SECRET
/my-product/prod/api/STRIPE_SECRET_KEY
/my-product/prod/web/NEXT_PUBLIC_API_URL
```

Then open the TUI:

```bash
aws-ssm-params --regions eu-north-1 --names-file names.txt tui
```

You immediately see which required parameters are present, missing, empty, or failing because of permissions/API errors.

### New environment onboarding

Create a names file for the new environment, open the TUI, fill missing values, and save them as encrypted `SecureString` parameters by default. Choose `String` or `StringList` only when plaintext configuration is intentional.

```bash
aws-ssm-params --profile staging --regions eu-north-1 --names-file names.txt tui
```

### Secret rotation

Open a secret, generate a random replacement, review it, and save it back to AWS SSM. The TUI can generate base64, hex, UUID, or custom-length base64 values.

### Multi-region audit

Scan the same expected parameter set across selected regions:

```bash
aws-ssm-params --regions eu-north-1,eu-central-1 --names-file names.txt tui
```

Or scan all enabled AWS regions:

```bash
aws-ssm-params --all-regions --names-file names.txt tui
```

This is useful for replicated services, disaster-recovery environments, and regional workloads.

### Backup, migration, and review

Export a known set of parameters:

```bash
aws-ssm-params --regions eu-north-1 --names-file names.txt export --to-file values.env
```

Import them into another profile or region:

```bash
AWS_PROFILE=target aws-ssm-params --regions eu-central-1 --names-file names.txt import --from-file values.env
```

> Exported files contain plaintext secrets. Store them carefully, encrypt them at rest, and delete them when they are no longer needed.

## Requirements

- Go 1.24+ to build from source.
- AWS credentials configured through environment variables, named profiles, SSO, instance role, or any other AWS SDK-supported method. AWS CLI is not required at runtime.
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

Tighten `Resource` ARNs to your own SSM name prefix when using this in production.

## Installation

### Install with Homebrew

The recommended installation method on macOS and Linux is Homebrew:

```bash
brew install biptec/tools/aws-ssm-params
```

Then verify the installation:

```bash
aws-ssm-params --help
aws-ssm-params tui --help
```

If you prefer to tap the repository first:

```bash
brew tap biptec/tools
brew install aws-ssm-params
```

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

Install the CLI/TUI with Homebrew:

```bash
brew install biptec/tools/aws-ssm-params
```

Create a names file:

```bash
cat > names.txt <<'EOF_NAMES'
/my-app/dev/api/DATABASE_URL
/my-app/dev/api/JWT_SECRET
/my-app/dev/api/STRIPE_SECRET_KEY
/my-app/dev/web/SESSION_SECRET
EOF_NAMES
```

Open the TUI without a names filter to browse everything you can see in the selected region:

```bash
aws-ssm-params --profile dev --regions eu-north-1 tui
```

Or scope it to a known application list:

```bash
aws-ssm-params --profile dev --regions eu-north-1 --names-file names.txt tui
```

Inside the TUI:

- Press `ctrl+/` to open context-sensitive `Shortcuts`.
- Press `/` to search/filter the list.
- Press `d` to show/hide full details for the selected parameter.
- Press `Enter` to edit the selected parameter.
- Press `n` to create a new parameter.
- Press `alt+e` on `Value` to clear, generate, load from file, or write to file.
- Press `alt+e` on `Policies` to clear, load from file, or write to file.
- Press `ctrl+s` in the editor to save.
- Press `esc` to go back; on the main screen, `esc` quits.

## Names file format

A names file is intentionally simple and safe to keep in Git because it stores only expected SSM parameter names, not secret values.

```text
# Comments are ignored
/my-app/dev/api/DATABASE_URL
/my-app/dev/api/JWT_SECRET

# Inline comments are also supported
/my-app/dev/api/STRIPE_SECRET_KEY # used by payments service
```

Rules:

- One SSM name per line.
- Empty lines are ignored.
- Full-line comments starting with `#` are ignored.
- Inline comments after a name are stripped.
- Names must start with `/`.
- Duplicate names are ignored.
- Names are sorted before display/export.

By default, `--names-file` is read-only. New parameters created in the TUI appear immediately in the current UI, but the file is not changed. Add `interactive --allow-names-file-update` or `tui --allow-names-file-update` when you want the TUI to append newly created names and remove deleted names from the file.

## Command map

A command is required. Running `aws-ssm-params` without a command prints CLI help instead of opening the TUI.

| Command | Purpose | Example |
| --- | --- | --- |
| [`interactive`](#interactive-tui) / `tui` | Open the terminal UI | `aws-ssm-params --regions eu-north-1 tui` |
| [`export`](#import-and-export) | Export values to dotenv/JSON | `aws-ssm-params --names-file names.txt export --to-file values.env` |
| [`import`](#import-and-export) | Import values from dotenv/JSON | `aws-ssm-params --names-file names.txt import --from-file values.env` |
| [`get`](#direct-getset) | Read one selected field from one parameter | `aws-ssm-params get /app/dev/api/JWT_SECRET --field value` |
| [`set`](#direct-getset) | Write one parameter value | `aws-ssm-params set /app/dev/api/LOG_LEVEL debug --type string` |

```text
aws-ssm-params [global options] <command> [command options]
aws-ssm-params [global options] interactive|tui [command options]
aws-ssm-params [global options] get <name> [--field field] [--file path]
aws-ssm-params [global options] set <name> <value> [command options]
aws-ssm-params [global options] set <name> --file path [command options]
aws-ssm-params [global options] import [--from-file path] [command options]
aws-ssm-params [global options] export [--format dotenv|json] [--to-file path] [--include-missing]
```

## Global scope flags

Global flags affect every command. They are applied before command-specific logic.

```text
--regions LIST      Comma-separated AWS regions. Use this flag once.
--all-regions       Search parameters across all enabled AWS regions.
--profile PROFILE   AWS profile name.
--no-color          Disable colored output.
--names-file FILE   File with SSM parameter names to load/filter.
--names LIST        Comma-separated SSM parameter names to load/filter.
--fields LIST       Comma-separated fields to load/show/import/export. Omitted means all fields.
--without-decryption
                    Load SecureString values without KMS decryption.
```

Environment variables:

```text
AWS_SSM_PARAMS_REGIONS
AWS_SSM_PARAMS_ALL_REGIONS
AWS_SSM_PARAMS_PROFILE
AWS_SSM_PARAMS_NAMES_FILE
AWS_SSM_PARAMS_NAMES
AWS_SSM_PARAMS_FIELDS
AWS_SSM_PARAMS_WITHOUT_DECRYPTION
AWS_SSM_PARAMS_NO_COLOR
```

### `--regions`

Use `--regions` once. Put multiple regions in the same comma-separated value:

```bash
aws-ssm-params --regions eu-north-1,eu-central-1 tui
```

These forms are intentionally rejected:

```bash
aws-ssm-params --regions eu-north-1 --regions eu-central-1 tui
```

`--regions` and `--all-regions` cannot be used together. If neither is provided, the tool falls back to `AWS_REGION`, `AWS_DEFAULT_REGION`, or AWS SDK profile configuration.

### `--names` and `--names-file`

`--names` and `--names-file` define the global SSM name scope. They can be combined; the resulting scope is the union.

```bash
aws-ssm-params --names /app/a,/app/b export
aws-ssm-params --names-file names.txt tui
```

If a scope is provided, commands operate only on those names. For example, if an import file contains ten records but `--names /app/a,/app/b` is set, only `/app/a` and `/app/b` are imported.

### `--fields`

`--fields` defines the global field scope.

If `--fields` is omitted, all fields are loaded, shown, imported, and exported.

If `--fields` is set, only the requested fields are available everywhere: API loading, table columns, Columns popup, details view, editor, import, and export. The `name` field is always kept internally so records can still be identified.

```bash
aws-ssm-params --fields name,type,value tui
aws-ssm-params --fields value export --to-file values.env
```

Supported field names:

```text
name
region
type
tier
data-type
policies
description
value
date
version
len
sha256
user
```

### `--without-decryption`

Use `--without-decryption` when you want metadata or encrypted-value placeholders but do not have KMS decrypt permissions. This disables `--with-decryption` for value reads so the command can still return what AWS allows.

## Parameter types and editable metadata

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
- `import --default-type` sets the default type for imported records that do not include type metadata.
- Dotenv `# type:` comments and typed JSON records take priority during import.

Advanced metadata supported by the editor and CLI:

```text
Tier          standard, advanced, intelligent-tiering
DataType      text, aws:ec2:image, aws:ssm:integration
Description   SSM parameter description
Policies      Advanced-tier parameter policies
Overwrite     Write option; shown only for new/missing parameters in the TUI
```

## Interactive TUI

Open the TUI explicitly with `interactive` or the short alias `tui`. Without `--names` or `--names-file`, it discovers all visible SSM parameters in the selected region(s):

```bash
aws-ssm-params --regions eu-north-1 tui
```

Use this mode for account inventory and cleanup: sort/filter the list, inspect details with `d`, and delete obsolete parameters when needed.

Useful TUI-only options:

```text
--show-columns LIST             Optional table columns to show on startup.
--sort FIELD,DIRECTION          Initial sort, for example name,asc or value,desc.
--keymap KEYMAP                 Keyboard navigation style: emacs or vi (default: emacs).
--allow-names-file-update       Allow the TUI to update --names-file on create/delete.
--show-secure-values            Show SecureString values by default in the TUI.
--no-confirm-overwrite-file     Skip local file overwrite confirmation in the TUI.
--no-confirm-write-securestring Skip plaintext SecureString write confirmation in the TUI.
--no-confirm-delete-one         Skip single-parameter delete confirmation in the TUI.
--no-confirm-delete-all         Skip visible-parameters delete confirmation in the TUI.
```

Interactive options have matching `AWS_SSM_PARAMS_*` environment variables such as `AWS_SSM_PARAMS_KEYMAP`, `AWS_SSM_PARAMS_SHOW_COLUMNS`, and `AWS_SSM_PARAMS_SHOW_SECURE_VALUES`.

### Main screen

The main screen starts as a full-height list:

```text
List of N Parameters
```

Press `d` to show the selected parameter details above the list. Details stay visible while you move through rows until you press `d` again.

`List of N Parameters` shows discovered names or the scoped names from `--names` / `--names-file`. When no names scope is provided, this is a full Parameter Store inventory for the selected region(s), useful for analysis and cleanup. Press `n` to create a new parameter; the editor opens with focus on `Name`. With `--names-file`, newly created names are shown immediately in the UI. The file itself is updated only when `--allow-names-file-update` is enabled.

### Footer and shortcuts

The bottom footer shows actions for the current screen. Navigation shortcuts are available in the context-sensitive `Shortcuts` popup opened with `ctrl+/`. When a popup is open, popup-specific actions move to the same bottom footer; the popup box itself stays focused on content and basic `Enter`/`Esc` actions.

Main footer when details are hidden:

```text
ctrl+/ help • enter edit • n new • d show details • / search • c columns • s sort • x delete • X delete visible • esc quit
```

Main footer when details are shown:

```text
ctrl+/ help • enter edit • n new • d hide details • / search • c columns • s sort • x delete • X delete visible • esc quit
```

Editor footer in Emacs keymap when `Value` is focused:

```text
ctrl+/ help • ctrl+s save • ctrl+l lines • alt+e value actions • esc back
```

Editor footer in Vi keymap, normal mode, when `Value` is focused:

```text
ctrl+/ help • i insert • ctrl+s save • ctrl+l lines • alt+e value actions • esc back
```

### Keymaps

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

Editor navigation in `--keymap vi` is modal. The editor opens in `NORMAL` mode, `i` enters `INSERT` mode, `esc` leaves `INSERT` mode, and a second `esc` goes back. Region, Type, Tier, DataType, and the new-parameter-only Overwrite field are selector fields opened with `Enter`; the focused selector is marked with `<`.

Vi editor shortcuts include:

```text
h / l                      backward/forward character
j / k                      next/previous line in Description/Policies/Value
PgDn / ctrl+f              page down in Description/Policies/Value
PgUp / ctrl+b              page up in Description/Policies/Value
w / b                      forward/backward word
0 / $                      start/end of line
gg / G                     start/end of text
x                          delete current character
D                          delete to end of real line
dw / db                    delete next/previous word
ctrl+l                     show/hide text area line numbers and gutters
```

Emacs editor shortcuts include:

```text
ctrl+f / ctrl+b            forward/backward character
ctrl+p / ctrl+n            previous/next line
PgDn / ctrl+v              page down in Description/Policies/Value
PgUp / alt+v               page up in Description/Policies/Value
ctrl+a / ctrl+e            start/end of line
ctrl+d                     delete current character
ctrl+k                     delete to end of real line / join next line
alt+d / alt+backspace      delete next/previous word
ctrl+l                     show/hide text area line numbers and gutters
```

### Columns and sorting

The `#` and `NAME` columns are always visible, and `NAME` is always the second column after `#`. Use `--show-columns` to choose startup columns, or press `c` in the TUI to open the Columns popup.

```bash
aws-ssm-params tui --show-columns region,type,value
AWS_SSM_PARAMS_SHOW_COLUMNS=region,type,value aws-ssm-params tui
```

Optional columns are rendered after `NAME` in this stable order:

```text
VALUE
TYPE
REGION
DATE
VERSION
TIER
LEN
SHA256
USER
DESCRIPTION
```

Press `s` to open the Sort popup, or use direct numeric sort hotkeys on the main screen. Numeric hotkeys map to currently visible columns, excluding `#`. The default sort is `NAME ↑`. Pressing the same sort hotkey again toggles ascending/descending. The sorted column header shows `↑` for ascending and `↓` for descending.

### Editor and file actions

The editor title is `New Parameter` for a new/missing parameter and `Edit Parameter` for an existing one.

`Description`, `Policies`, and `Value` share the same expandable editor component. A short one-line value stays inline with its label. Press `Enter` to expand it into a text area and insert a newline at the current cursor position. Pasting multiline content or typing beyond the available width also expands the field. If an expanded field is edited back to one line that fits the screen, it collapses back to the compact inline layout.

In text area mode, `ctrl+l` toggles line numbers and gutters so the raw multiline value starts at the left edge and can be copied from the terminal without line-number prefixes.

`alt+e value actions` opens a single-choice popup for `Value`: clear, random, load from file, and write to file. When `Policies` is focused, `alt+e policies actions` opens clear, load from file, and write to file, without random generation.

Write-to-file confirms risky actions. SecureString plaintext and existing-file overwrite confirmations are shown directly over the `Write to file` popup, and `Esc` returns to the file path input so the path can be changed. If editor fields changed, `Esc` shows `Unsaved changes. Discard unsaved changes?` before leaving.

For Advanced tier parameters, `Policies` is formatted as readable JSON in the editor. If AWS returns policy metadata with `PolicyStatus`, `PolicyText`, and `PolicyType`, the editor unwraps `PolicyText` and shows only the editable policy JSON. Saving sends that canonical policy array back to AWS instead of the read-only metadata wrapper.

## Import and export

Supported formats:

```text
dotenv
json
```

The default format is `dotenv`.

Global `--names` / `--names-file` and `--fields` apply to import/export too. For example, if an import file contains ten records but `--names` lists two, only those two are imported. If `--fields value,type` is set, other fields from the file are ignored.

### Dotenv export

```bash
aws-ssm-params --regions eu-north-1 --names-file names.txt export --to-file values.env
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

The `# ssm:` comment is the canonical SSM name. The optional `# type:` comment preserves the AWS SSM parameter type. The variable name is a readable alias.

During import, the `# ssm:` comment takes priority. If the comment is missing, the tool tries to resolve the variable name from the names file. Ambiguous aliases are rejected.

### JSON export

```bash
aws-ssm-params --regions eu-north-1 --names-file names.txt export --format json --to-file values.json
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

JSON uses SSM names as keys, so it can be imported without alias resolution. JSON export always uses object records, even when only `value` is exported. The compact legacy JSON shape `{ "/path": "value" }` is still accepted for imports.

### Import

Dotenv import requires `--names-file` because dotenv keys are aliases and the tool needs the names file to resolve them back to full SSM names:

```bash
aws-ssm-params --regions eu-north-1 --names-file names.txt import --from-file values.env
```

JSON import does not require `--names-file`, because JSON uses full SSM names as keys:

```bash
aws-ssm-params --regions eu-north-1 import --format json --from-file values.json
```

You may still pass `--names-file names.txt` with JSON if you want to keep imports scoped to a known name set.

By default, import will not overwrite existing non-empty values. Imported records with type metadata keep that type. Records without metadata preserve the existing AWS type, or use `SecureString` for new parameters. You can set defaults:

```bash
aws-ssm-params --regions eu-north-1 --names-file names.txt import --from-file app.env --default-type string
aws-ssm-params --regions eu-north-1 import --format json --from-file values.json --default-type string
```

To overwrite existing non-empty values intentionally:

```bash
aws-ssm-params --regions eu-north-1 --names-file names.txt import --from-file values.env --default-override
aws-ssm-params --regions eu-north-1 import --format json --from-file values.json --default-override
```

## Direct get/set

Read one parameter value. `get` is intentionally single-field output for shell scripts; the default field is `value`:

```bash
aws-ssm-params --regions eu-north-1 get /my-app/dev/api/JWT_SECRET
aws-ssm-params --regions eu-north-1 get /my-app/dev/api/JWT_SECRET --field value
```

Read one metadata field as plain text:

```bash
aws-ssm-params --regions eu-north-1 get /my-app/dev/api/JWT_SECRET --field type
aws-ssm-params --regions eu-north-1 get /my-app/dev/api/JWT_SECRET --field version
aws-ssm-params --regions eu-north-1 get /my-app/dev/api/JWT_SECRET --field description
```

If global `--fields` is set, `--field` must be included in that scope. For example, `--fields type get ... --field value` is rejected because `value` was not loaded.

Write the selected field to a file instead of stdout:

```bash
aws-ssm-params --regions eu-north-1 get /my-app/dev/api/TLS_KEY --field value --file tls.key
```

Set one value. Existing parameters keep their current AWS type when `--type` is omitted; new parameters are created as `SecureString` by default:

```bash
aws-ssm-params --regions eu-north-1 set /my-app/dev/api/JWT_SECRET "new-secret-value"
```

Set a plaintext configuration value intentionally:

```bash
aws-ssm-params --regions eu-north-1 set /my-app/dev/api/LOG_LEVEL debug --type string
```

Set a comma-separated StringList value:

```bash
aws-ssm-params --regions eu-north-1 set /my-app/dev/api/ALLOWED_HOSTS "api.example.com,www.example.com" --type string-list
```

Set one value from a file:

```bash
aws-ssm-params --regions eu-north-1 set /my-app/dev/api/TLS_KEY --file tls.key
```

Overwrite an existing non-empty value:

```bash
aws-ssm-params --regions eu-north-1 set /my-app/dev/api/JWT_SECRET "new-secret-value" --override
```

Additional `set` flags:

```text
--type VALUE          string, string-list, secure-string
--tier VALUE          standard, advanced, intelligent-tiering
--data-type VALUE     text, aws:ec2:image, aws:ssm:integration
--description VALUE   SSM parameter description
--policies VALUE      Advanced-tier parameter policies JSON
--policies-file FILE  Read policies JSON from file
--region VALUE        Target AWS region for this write
--override            Overwrite existing non-empty values
```

## Region modes

### Single region

```bash
aws-ssm-params --regions eu-north-1 tui
```

All visible SSM parameters are discovered in `eu-north-1`. Add `--names-file names.txt` to check only the names listed in that file.

### Selected regions

```bash
aws-ssm-params --regions eu-north-1,eu-central-1 tui
```

Parameters are discovered in each selected region. With `--names-file names.txt`, each listed name is searched in the selected regions; existing parameters are shown as regional rows and names missing from every scanned region are shown as wildcard missing rows.

### All enabled regions

```bash
aws-ssm-params --all-regions tui
```

The tool calls `ec2:DescribeRegions`, filters out not-opted-in regions, and scans the remaining enabled regions. Add `--names-file names.txt` to filter the scan to a known set of names.

## Security notes

- Values are hidden by default in the TUI.
- Pressing `v` reveals cached values on screen; be careful when screen sharing or recording.
- `--show-secure-values` shows SecureString values by default in the TUI; use it only in trusted terminals.
- `--without-decryption` can help when you have read permissions but not KMS decrypt permissions.
- Exported dotenv/JSON files contain plaintext secrets.
- Use restrictive file permissions and encrypted storage for exported files.
- Prefer short-lived AWS credentials or SSO sessions.
- Scope IAM policies to your SSM name prefix when possible.
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

It is intentionally small, script-friendly, and designed to fit into existing AWS workflows.
