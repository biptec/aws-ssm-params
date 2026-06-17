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
- Can discover SSM parameters directly from AWS, or read an optional paths file with one SSM parameter name per line.
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
aws-ssm-params --regions eu-north-1 --names-file paths.txt
```

You immediately see which required parameters are present, missing, empty, or failing due to AWS permission/API errors.

### New environment onboarding

Create a list of expected names for a new environment, open the TUI, fill missing values, and save them as encrypted `SecureString` parameters by default, or choose `String` / `StringList` when plaintext parameters are intentional.

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
aws-ssm-params --regions eu-north-1 --regions eu-central-1 --names-file paths.txt
```

Or scan all AWS regions enabled for the account:

```bash
aws-ssm-params --all-regions --names-file paths.txt
```

This is useful when you need to verify whether replicated apps, disaster-recovery regions, or regional workloads have the same required secret set.

### Backup, migration, and review

Export a known set of parameters:

```bash
aws-ssm-params --regions eu-north-1 --names-file paths.txt export --to-file values.env
```

Import them into another account/profile/region:

```bash
AWS_PROFILE=target aws-ssm-params --regions eu-central-1 --names-file paths.txt import --from-file values.env
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

Tighten `Resource` ARNs to your own name prefix when using this in production.

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
aws-ssm-params --profile dev --regions eu-north-1 --names-file paths.txt interactive
```

Inside the TUI:

- Press `ctrl+/` to open the context-sensitive `Shortcuts` page.
- Press `d` on the main screen to show/hide full details for the selected parameter.
- Press `Enter` on the main screen to edit a parameter, or `n` to create a new parameter. The editor exposes the editable SSM fields: name, region, type, tier, data type, description, and value. `Description`, `Policies`, and `Value` render as compact one-line fields when the content is short, and expand to text areas when the content is multiline, too long for the row, or opened with `Enter`. `Policies` is shown only for `Advanced` tier parameters. `Overwrite` is shown only while creating a new/missing parameter and defaults to `false`.
- Press `alt+e` while the `Value` field is focused to open value actions: clear, random, load from file, or write to file. Press `alt+e` while `Policies` is focused to open policies actions: clear, load from file, or write to file. Choose an action with the focused `(*)` row, then press `ctrl+s` to save value changes.
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

- One SSM name per line.
- Empty lines are ignored.
- Full-line comments starting with `#` are ignored.
- Inline comments after a name are stripped.
- Names must start with `/`.
- Duplicate names are ignored.
- Names are sorted before display/export.

This makes it easy to keep parameter requirements in Git without storing any secret values.

## Command overview

```text
aws-ssm-params [global options] <command> [command options]
aws-ssm-params [global options] interactive|tui [command options]
aws-ssm-params [global options] get <name> [--file path]
aws-ssm-params [global options] set <name> <value> [command options]
aws-ssm-params [global options] set <name> --file path [command options]
aws-ssm-params [global options] import [--from-file path] [command options]
aws-ssm-params [global options] export [--format dotenv|json] [--to-file path] [--include-missing]
```

A command is now required. Running `aws-ssm-params` without a command prints CLI help instead of opening the TUI.

Open the TUI explicitly with `interactive` (or the short alias `tui`). Without `--names-file` or `--names`, the TUI discovers parameters directly from AWS for the selected region(s):

```bash
aws-ssm-params --regions eu-north-1 interactive
```

Use `--names-file` or `--names` when you want to filter the TUI to a known set of names:

```bash
aws-ssm-params --regions eu-north-1 --names-file names.txt interactive
aws-ssm-params --regions eu-north-1 --names /app/a,/app/b interactive
```

By default, `--names-file` is treated as a read-only filter/list. New parameters created in the TUI appear immediately in the current UI, but the file is not changed. Add `interactive --allow-names-file-update` when you want the TUI to append newly created names and remove deleted names from that file.

## Global options

```text
--regions REGION    AWS regions. Repeat or comma-separate to scan selected regions.
--all-regions       Search parameters across all enabled AWS regions.
--profile PROFILE   AWS profile name.
--no-color          Disable colored output.
--names-file FILE   Optional file with SSM parameter names to load/filter.
--names LIST        Comma-separated SSM parameter names to load/filter.
--fields LIST       Comma-separated fields to load/show/import/export. Omitted means all fields.
--without-decryption
                    Load SecureString values without KMS decryption.
```

Interactive-only options:

```text
--show-columns LIST             Comma-separated optional TUI columns to show on startup.
--sort FIELD,DIRECTION          Initial sort, for example name,asc or value,desc.
--keymap KEYMAP                 Keyboard navigation style: emacs or vi (default: emacs).
--allow-names-file-update       Allow the TUI to update --names-file on create/delete.
--show-secure-values            Show SecureString values by default in the TUI.
--no-confirm-overwrite-file     Skip local file overwrite confirmation in the TUI.
--no-confirm-write-securestring Skip plaintext SecureString write confirmation in the TUI.
--no-confirm-delete-one         Skip single-parameter delete confirmation in the TUI.
--no-confirm-delete-all         Skip visible-parameters delete confirmation in the TUI.
```

All global options can also be configured through environment variables:

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

Interactive options have matching `AWS_SSM_PARAMS_*` environment variables such as `AWS_SSM_PARAMS_KEYMAP`, `AWS_SSM_PARAMS_SHOW_COLUMNS`, and `AWS_SSM_PARAMS_SHOW_SECURE_VALUES`.

Notes:

- `--regions` and `--all-regions` cannot be used together.
- If `--regions` is omitted, the tool falls back to `AWS_REGION`, `AWS_DEFAULT_REGION`, or AWS CLI profile configuration.
- `--regions` accepts one region or multiple comma-separated regions.
- `--names` and `--names-file` define the global parameter-name scope. `--names` and `--names-file` can be combined; the resulting scope is the union.
- `--fields` defines the global field scope. If omitted, all fields are loaded, displayed, imported, and exported. If set, only the requested fields are available everywhere. `name` is always kept internally so records can still be identified.
- Direct `get`, `set`, and `import` operate on one concrete region.
- `interactive` and `export` support repeated/comma-separated `--regions` and `--all-regions`.
- `interactive --allow-names-file-update` requires `--names-file`.
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
- `import --default-type` sets the default type for imported records that do not include type metadata.
- Dotenv `# type:` comments and typed JSON records take priority during import.

Supported CLI aliases are `secure-string`, `string`, and `string-list`.

## Interactive TUI

The TUI is built for fast keyboard-driven maintenance.

The main screen starts as a single full-height list:

```text
List of N Parameters
```

Press `d` to show `Selected Parameter` above the list. It opens directly in full-detail mode and stays visible while you move through the parameter list until you press `d` again.

`List of N Parameters` shows all discovered or filtered names with optional metadata columns. Press `n` to create a new parameter; the editor opens with focus on `Name`. With `--names-file`, newly created names are shown immediately in the UI. The file itself is updated only when `--allow-names-file-update` is enabled.

### Managed paths file updates

When `--names-file` is used without additional flags, it is read-only:

```text
create -> parameter appears in the current UI, names-file unchanged
delete -> parameter is deleted in AWS and remains as a missing row, names-file unchanged
```

Use `--allow-names-file-update` to let the TUI keep the paths file in sync with create/delete operations:

```bash
aws-ssm-params --names-file paths.txt --allow-names-file-update
```

Then:

```text
create -> append name to names-file if missing
delete -> remove path from names-file and remove the row from the UI
```

Without `--names-file`, deleted rows disappear from the UI because the list is discovered directly from AWS.

### Footer shortcuts

The footer only shows action shortcuts. Navigation shortcuts are available in the context-sensitive `Shortcuts` popup opened with `ctrl+/`. The `ctrl+/ help` shortcut is always shown first so it remains visible in narrow terminals. When a popup is open, popup-specific actions are shown in the same bottom screen footer, while the popup box itself stays focused on the content.

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

Editor footer in Emacs keymap when `Policies` is focused:

```text
ctrl+/ help • ctrl+s save • ctrl+l lines • alt+e policies actions • esc back
```

Editor footer in Vi keymap, normal mode, when `Value` is focused:

```text
ctrl+/ help • i insert • ctrl+s save • ctrl+l lines • alt+e value actions • esc back
```

Editor footer in Vi keymap, insert mode, when `Value` is focused:

```text
ctrl+/ help • ctrl+s save • ctrl+l lines • alt+e value actions • esc normal
```

`alt+e value actions` opens a single-choice popup for `Value`-specific operations: clear, random, load from file, and write to file. When `Policies` is focused, `alt+e policies actions` opens the same style of popup for clear, load from file, and write to file, without random generation. Load/write file paths are entered inside the corresponding popup, not as a permanent field on the editor screen. Popup boxes show the basic confirmation actions inside the frame, such as `Enter select   Esc cancel`; the key names are highlighted while the action descriptions are muted. The full popup hotkeys are shown in the bottom screen footer and in `Shortcuts`, not beside individual rows. Opening a child popup normally replaces the previous popup instead of stacking it; `Esc` returns directly to the editor/page. The exception is write-to-file confirmation: SecureString plaintext and existing-file overwrite confirmations are shown directly over the `Write to file` popup, and `Esc` returns to the file path input so the path can be changed. If editor fields changed, `Esc` shows `Unsaved changes. Discard unsaved changes?` before leaving. Single-choice popups use `( )` / `(*)`; multi-choice popups use `[ ]` / `[x]`.

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

Editor navigation in `--keymap vi` is modal. The editor opens in `NORMAL` mode, `i` enters `INSERT` mode, `esc` leaves `INSERT` mode, and a second `esc` goes back. While inserting, the active editable text-field label shows `[INSERT]`, for example `Name [INSERT]:`, `Policies [INSERT]:`, or `Value [INSERT]:`. Region, Type, Tier, DataType, and the new-parameter-only Overwrite field are selector fields opened with `Enter`; the focused selector is marked with `<`.

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

Hidden duplicate back/quit shortcuts such as `q` and `ctrl+g` remain available and are documented in `Shortcuts`, but the footer shows `esc` as the primary back/quit key.

### Columns and sorting

The `#` and `NAME` columns are always visible, and `NAME` is always the second column after `#`. Use `interactive --show-columns` to choose startup columns, or press `c` in the TUI to open the Columns popup over the current main screen. The popup lets you choose optional columns populated from AWS SSM metadata/runtime state. Use `Space` to toggle the focused `[x]` row and immediately preview the table behind the popup, `Enter` to apply the visible preview, and `Esc` to roll back to the columns that were visible before the popup opened. Optional columns are rendered after `NAME` in this stable order:

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

Press `s` to open the Sort popup, or use direct numeric sort hotkeys on the main screen. Numeric hotkeys map to the currently visible columns, excluding `#`. The default sort is `NAME ↑`, so the active sort arrow is visible immediately after startup. Pressing the same sort hotkey again toggles the direction between ascending and descending. The sorted column header shows `↑` for ascending and `↓` for descending. For example, when the table shows `# NAME VALUE TYPE REGION`, the active sort hotkeys are:

```text
1 Name
2 Value
3 Type
4 Region
```

The Sort popup itself shows only visible sortable columns with a focused `(*)` row and the current direction arrow on the active column, for example `Name ↑`. Letter hotkeys are shown in the bottom screen footer and in `Shortcuts` when `ctrl+/` is opened from the Sort popup. `Enter` sorts the selected column; if that column is already active, it toggles `ASC`/`DESC`. `d` toggles/applies the selected row direction:

```text
n Name
v Value
t Type
r Region
a Date
o Version
i Tier
z Len
s SHA256
u User
e Description
d Direction
```

### Expandable text fields

`Description`, `Policies`, and `Value` share the same expandable editor component. A short one-line value stays inline with its label. Press `Enter` to expand the compact field into a text area and insert a newline at the current cursor position. Pasting multiline content or typing beyond the available width also expands the field into a text area. If an expanded field is edited back to one line that fits the screen, it collapses back to the compact inline layout. In text area mode, `ctrl+l` toggles line numbers and gutters so the raw multiline value starts at the left edge and can be selected and copied from the terminal without line-number or gutter prefixes.

For Advanced tier parameters, `Policies` is formatted as readable JSON in the editor. If AWS returns policy metadata with `PolicyStatus`, `PolicyText`, and `PolicyType`, the editor unwraps `PolicyText` and shows only the editable policy JSON. Saving sends that canonical policy array back to AWS instead of the read-only metadata wrapper.

Examples:

```bash
aws-ssm-params interactive --show-columns region,type,value
AWS_SSM_PARAMS_SHOW_COLUMNS=region,type,value aws-ssm-params interactive
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
aws-ssm-params --regions eu-north-1 --names-file paths.txt export --to-file values.env
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

During import, the `# ssm:` comment takes priority. If the comment is missing, the tool tries to resolve the variable name from the paths file. Ambiguous aliases are rejected.

### JSON export

```bash
aws-ssm-params --regions eu-north-1 --names-file paths.txt export --format json --to-file values.json
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

JSON uses SSM names as keys, so it can be imported without alias resolution. The compact legacy JSON shape `{ "/path": "value" }` is still accepted for imports; typed exports use `{ "type": "...", "value": "..." }` records when type metadata is available.

### Import

Dotenv import requires `paths.txt` because dotenv keys are aliases and the tool needs the paths file to resolve them back to full SSM names:

```bash
aws-ssm-params --regions eu-north-1 --names-file paths.txt import --from-file values.env
```

JSON import does not require `paths.txt`, because JSON uses full SSM names as keys:

```bash
aws-ssm-params --regions eu-north-1 import --format json --from-file values.json
```

You may still pass `--names-file paths.txt` with JSON if you want to keep imports scoped to a known name set, but it is optional for that format:

```bash
aws-ssm-params --regions eu-north-1 --names-file paths.txt import --format json --from-file values.json
```

By default, import will not overwrite existing non-empty values. Imported records with type metadata keep that type. Records without metadata preserve the existing AWS type, or use `SecureString` for new parameters. You can set a different default type:

```bash
aws-ssm-params --regions eu-north-1 --names-file paths.txt import --from-file app.env --default-type string
```

For JSON without `paths.txt`:

```bash
aws-ssm-params --regions eu-north-1 import --format json --from-file values.json --default-type string
```

To overwrite existing non-empty values intentionally:

```bash
aws-ssm-params --regions eu-north-1 --names-file paths.txt import --from-file values.env --default-override
aws-ssm-params --regions eu-north-1 import --format json --from-file values.json --default-override
```

## Direct get/set

Read one parameter:

```bash
aws-ssm-params --regions eu-north-1 get /my-app/dev/api/JWT_SECRET
```

Write the value to a file instead of stdout:

```bash
aws-ssm-params --regions eu-north-1 get /my-app/dev/api/TLS_KEY --file tls.key
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

All writes use:

```text
Type: existing type, explicit --type, imported type metadata, or SecureString by default
Tier: editor-selected tier, imported metadata tier, or Intelligent-Tiering by default
DataType: editor-selected data type, imported metadata data type, or text by default
Policies: editor-entered parameter policies only for Advanced tier parameters
Overwrite: true after protection checks pass; in the TUI this option is shown only when creating a new/missing parameter and defaults to false
```

## Region modes

### Single region

```bash
aws-ssm-params --regions eu-north-1 interactive
```

All visible SSM parameters are discovered in `eu-north-1`. Add `--names-file names.txt` to check only the names listed in that file.

### Selected regions

```bash
aws-ssm-params --regions eu-north-1 --regions eu-central-1 interactive
```

Parameters are discovered in each selected region. With `--names-file names.txt`, each listed name is searched in the selected regions; existing parameters are shown as regional rows and paths missing from every scanned region are shown as wildcard missing rows.

### All enabled regions

```bash
aws-ssm-params --all-regions
```

The tool calls `ec2:DescribeRegions`, filters out not-opted-in regions, and scans the remaining enabled regions. Add `--names-file paths.txt` to filter the scan to a known set of names.

## Security notes

- Values are hidden by default in the TUI.
- Pressing `v` reveals cached values on screen; it is documented on `Shortcuts` instead of the footer. Be careful when screen sharing or recording.
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

It is intentionally small, script-friendly, and designed to fit into existing AWS CLI workflows.
