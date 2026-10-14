# Recurring duties

`secretary` has no "repeat every week" field and will not get one: the artifact describes what
you owe, not the schedule that produces it. Anything regular lives outside — in `cron` or
`launchd`, which calls the CLI on the right day.

## The example: a weekly report

- `scripts/weekly-report.sh` adds "Write the weekly report", due on the coming Friday, tier 2,
  effort 2h, label `weekly-report`.
- The script **does not add a second copy while the previous one is open**: it checks
  `list --label weekly-report` first. A column of identical lines is exactly what makes a list
  stop being read.
- It calls `secretary` from `PATH`, or whatever `SECRETARY_BIN` points at, and reads the data
  home from `SECRETARY_HOME` like any other invocation.

### cron

```cron
0 9 * * 1  SECRETARY_HOME=$HOME/.secretary /path/to/scripts/weekly-report.sh >> $HOME/.secretary/recurring.log 2>&1
```

### launchd (macOS)

Save a property list under `~/Library/LaunchAgents/`, for example
`com.example.secretary.weekly-report.plist`, with `ProgramArguments` pointing at the script,
`StartCalendarInterval` set to `Weekday 1, Hour 9`, and `EnvironmentVariables` carrying
`SECRETARY_HOME` and a `PATH` that contains the binary — `launchd` has almost no `PATH` of its own.
Then:

```sh
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.example.secretary.weekly-report.plist
launchctl print gui/$(id -u)/com.example.secretary.weekly-report      # state and schedule
launchctl kickstart -k gui/$(id -u)/com.example.secretary.weekly-report  # run it now
launchctl bootout gui/$(id -u)/com.example.secretary.weekly-report    # switch it off
```

The property list stays outside the repository: it is a setting of the machine, not of the project.

## Adding another one

Copy the script, change `--title`, the label and the day, and register a second entry. Two
scripts with two labels never interfere: each one only checks its own label.
