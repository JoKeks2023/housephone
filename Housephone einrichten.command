#!/bin/zsh
# Housephone einrichten: richtet das Xcode-Projekt für dein Apple-Team ein.
#
# Doppelklick im Finder öffnet das Skript im Terminal. Es fragt Team und
# Bundle-ID-Präfix ab und schreibt beides nach ios/Config/Local.xcconfig
# (nicht eingecheckt). Die project.pbxproj bleibt unverändert.
#
# Ohne Rückfragen (z. B. CI):
#   ./Housephone\ einrichten.command --team ABCDE12345 --prefix org.example
#
# Keine Abhängigkeiten außer macOS und Xcode.

emulate -L zsh
setopt no_unset pipe_fail

SCRIPT_DIR=${0:A:h}
cd "$SCRIPT_DIR" || exit 1

PROJECT="ios/Housephone.xcodeproj"
BASE_CONFIG="ios/Config/Housephone.xcconfig"
LOCAL_CONFIG="ios/Config/Local.xcconfig"
ADR_LOCAL_PUSH="docs/architecture/ADR-0005-modus-ohne-bridge.md"

# MARK: - Helpers (no terminal needed)

valid_team() { [[ $1 =~ '^[A-Z0-9]{10}$' ]]; }

valid_prefix() {
  [[ $1 =~ '^[A-Za-z][A-Za-z0-9-]*(\.[A-Za-z0-9][A-Za-z0-9-]*)+$' ]] && [[ $1 != *.housephone ]]
}

# Reads KEY from an xcconfig file ("KEY = value").
config_value() {
  local file=$1 key=$2 line
  [[ -f $file ]] || return 1
  while IFS= read -r line; do
    if [[ $line =~ "^[[:space:]]*${key}[[:space:]]*=[[:space:]]*([^[:space:]/]+)" ]]; then
      print -r -- $match[1]
      return 0
    fi
  done < $file
  return 1
}

current_team() { config_value $LOCAL_CONFIG DEVELOPMENT_TEAM || config_value $BASE_CONFIG DEVELOPMENT_TEAM; }
current_prefix() { config_value $LOCAL_CONFIG HOUSEPHONE_BUNDLE_PREFIX || config_value $BASE_CONFIG HOUSEPHONE_BUNDLE_PREFIX; }

write_local_config() {
  local team=$1 prefix=$2
  mkdir -p ${LOCAL_CONFIG:h}
  cat > $LOCAL_CONFIG <<EOF
// Written by "Housephone einrichten.command". Not checked in.
// Run the script again to change these values.
DEVELOPMENT_TEAM = $team
HOUSEPHONE_BUNDLE_PREFIX = $prefix
EOF
}

# "TEAMID<TAB>Team name" for every signing certificate in the keychain.
detect_teams() {
  local -A seen
  local name pem subject team org tmp
  tmp=$(mktemp -d) || return 0
  security find-identity -v -p codesigning 2>/dev/null \
    | sed -n 's/^ *[0-9]*) [0-9A-F]* "\(.*\)"$/\1/p' \
    | grep -E '^(Apple Development|Apple Distribution|iPhone Developer|iPhone Distribution|Developer ID Application):' \
    | while IFS= read -r name; do
        security find-certificate -a -c "$name" -p 2>/dev/null \
          | awk -v dir="$tmp" '/BEGIN CERT/{n++} {print > (dir "/" n ".pem")}'
        for pem in $tmp/*.pem(N); do
          subject=$(openssl x509 -noout -subject -in $pem 2>/dev/null)
          rm -f $pem
          [[ $subject =~ 'OU ?= ?([A-Z0-9]{10})' ]] || continue
          team=$match[1]
          org=""
          if [[ $subject =~ '(^|[/,] ?)O ?= ?([^/,]+)' ]]; then org=$match[2]; fi
          [[ -n ${seen[$team]-} ]] && continue
          seen[$team]=1
          print -r -- "$team	$org"
        done
      done
  rm -rf $tmp
}

suggested_prefix() {
  local user=${(L)$(id -un)}
  user=${user//[^a-z0-9]/}
  [[ -z $user ]] && user=developer
  [[ $user == [0-9]* ]] && user="u$user"
  print -r -- "com.$user"
}

# MARK: - Non-interactive mode

if (( $# > 0 )); then
  team="" prefix=""
  while (( $# > 0 )); do
    case $1 in
      --team) team=${2-}; shift 2 ;;
      --prefix) prefix=${2-}; shift 2 ;;
      -h|--help)
        print "Aufruf: ${0:t} [--team TEAMID --prefix com.example]"
        print "Ohne Argumente startet die interaktive Einrichtung."
        exit 0 ;;
      *) print -u2 "Unbekannte Option: $1"; exit 2 ;;
    esac
  done
  team=${(U)team}
  valid_team "$team" || { print -u2 "Ungültige Team-ID: '$team' (10 Zeichen, A–Z und 0–9)"; exit 2; }
  valid_prefix "$prefix" || { print -u2 "Ungültiges Präfix: '$prefix' (z. B. com.example)"; exit 2; }
  write_local_config $team $prefix
  print "$LOCAL_CONFIG geschrieben: Team $team, Präfix $prefix"
  print "apns.topic für die Bridge: $prefix.housephone.voip"
  exit 0
fi

# MARK: - Terminal UI

if [[ ! -t 0 || ! -t 1 ]]; then
  print -u2 "Bitte im Terminal starten (oder --team/--prefix angeben)."
  exit 2
fi

ESC=$'\e'
BOLD="${ESC}[1m" DIM="${ESC}[2m" RESET="${ESC}[0m"
ACCENT="${ESC}[38;5;39m" GREEN="${ESC}[38;5;42m" RED="${ESC}[38;5;203m" YELLOW="${ESC}[38;5;221m"
SAVED_STTY=$(stty -g)

restore_terminal() {
  stty $SAVED_STTY 2>/dev/null
  print -n "${ESC}[?25h${ESC}[?1049l"
}

finish() {
  restore_terminal
  exit ${1:-0}
}

trap 'finish 130' INT TERM HUP
trap 'restore_terminal' EXIT

print -n "${ESC}[?1049h${ESC}[?25l"

TOTAL_STEPS=4

screen() {
  local step=$1 title=$2 i bar=""
  print -n "${ESC}[H${ESC}[2J"
  print ""
  print "  ${ACCENT}${BOLD}☎  Housephone einrichten${RESET}"
  print "  ${DIM}Xcode-Projekt für dein Apple-Team vorbereiten${RESET}"
  print ""
  if (( step > 0 )); then
    for (( i = 1; i <= TOTAL_STEPS; i++ )); do
      if (( i < step )); then bar+="${GREEN}●${RESET} "
      elif (( i == step )); then bar+="${ACCENT}●${RESET} "
      else bar+="${DIM}○${RESET} "; fi
    done
    print "  $bar ${DIM}Schritt $step von $TOTAL_STEPS${RESET}"
    print ""
  fi
  print "  ${BOLD}$title${RESET}"
  print ""
}

# Reads one key; sets KEY to up, down, enter, quit or the character.
read_key() {
  local k rest
  read -rsk1 k
  case $k in
    $ESC)
      rest=""
      read -rsk2 -t 0.05 rest
      case $rest in
        "[A"|"OA") KEY=up ;;
        "[B"|"OB") KEY=down ;;
        *) KEY=quit ;;
      esac ;;
    $'\n'|$'\r') KEY=enter ;;
    k) KEY=up ;;
    j) KEY=down ;;
    q) KEY=quit ;;
    *) KEY=$k ;;
  esac
}

# menu STEP TITLE DEFAULT_INDEX HINT ITEMS... → CHOICE (1-based)
menu() {
  local step=$1 title=$2 sel=$3 hint=$4
  shift 4
  local -a items=("$@")
  local i
  while true; do
    screen $step "$title"
    [[ -n $hint ]] && { print -r -- "$hint"; print ""; }
    for (( i = 1; i <= ${#items}; i++ )); do
      if (( i == sel )); then
        print "  ${ACCENT}${BOLD}❯ ${items[i]}${RESET}"
      else
        print "    ${items[i]}"
      fi
    done
    print ""
    print "  ${DIM}↑/↓ auswählen · Enter bestätigen · Esc abbrechen${RESET}"
    read_key
    case $KEY in
      up) (( sel = sel > 1 ? sel - 1 : ${#items} )) ;;
      down) (( sel = sel < ${#items} ? sel + 1 : 1 )) ;;
      enter) CHOICE=$sel; return 0 ;;
      quit) finish 1 ;;
      [1-9]) (( KEY <= ${#items} )) && sel=$KEY ;;
    esac
  done
}

# prompt STEP TITLE TEXT → INPUT (empty = back)
prompt() {
  local step=$1 title=$2 text=$3
  screen $step "$title"
  print -r -- "$text"
  print ""
  print "  ${DIM}Leer lassen und Enter = zurück${RESET}"
  print ""
  print -n "${ESC}[?25h"
  stty $SAVED_STTY
  INPUT=""
  read -r "INPUT?  ❯ "
  print -n "${ESC}[?25l"
}

wait_key() {
  print ""
  print "  ${DIM}Taste drücken zum Fortfahren …${RESET}"
  read -rsk1 _
}

# MARK: - Step 1: Mac and Xcode

screen 1 "Mac und Xcode prüfen"
macos=$(sw_vers -productVersion 2>/dev/null)
xcode_path=$(xcode-select -p 2>/dev/null)
xcode_version=$(xcodebuild -version 2>/dev/null | head -1)
ok=1
print "  ${GREEN}✓${RESET} macOS ${macos:-unbekannt}"
if [[ -n $xcode_version ]]; then
  print "  ${GREEN}✓${RESET} $xcode_version ${DIM}($xcode_path)${RESET}"
else
  ok=0
  print "  ${RED}✗${RESET} Xcode nicht gefunden"
  print ""
  print "  Installiere Xcode aus dem App Store und öffne es einmal."
  print "  Liegt es woanders: ${BOLD}sudo xcode-select -s /Applications/Xcode.app${RESET}"
fi
if [[ -d $PROJECT && -f $BASE_CONFIG ]]; then
  print "  ${GREEN}✓${RESET} Projekt ${DIM}$PROJECT${RESET}"
else
  ok=0
  print "  ${RED}✗${RESET} $PROJECT oder $BASE_CONFIG fehlt. Liegt das Skript im Housephone-Ordner?"
fi
print ""
old_team=$(current_team)
old_prefix=$(current_prefix)
if [[ -f $LOCAL_CONFIG ]]; then
  print "  Bisher eingerichtet: Team ${BOLD}${old_team}${RESET}, Präfix ${BOLD}${old_prefix}${RESET}"
else
  print "  ${YELLOW}Noch nicht eingerichtet${RESET} ${DIM}(Standard: Team ${old_team}, Präfix ${old_prefix})${RESET}"
fi
if (( ! ok )); then
  wait_key
  finish 1
fi
wait_key

# MARK: - Steps 2–4

print -n "${ESC}[H${ESC}[2J"
print "\n  ${DIM}Suche Signier-Zertifikate im Schlüsselbund …${RESET}"
teams=("${(@f)$(detect_teams)}")
teams=(${teams:#})

team=$old_team
prefix=$old_prefix
step=2
while true; do
  case $step in
    2)
      labels=() ids=() sel=1
      for entry in $teams; do
        id=${entry%%$'\t'*}
        org=${entry#*$'\t'}
        ids+=$id
        labels+="${org:-Team} ${DIM}($id)${RESET}"
        [[ $id == $team ]] && sel=${#ids}
      done
      if [[ -n $team && ${ids[(Ie)$team]} -eq 0 ]]; then
        ids=($team $ids)
        labels=("Bisheriges Team ${DIM}($team)${RESET}" $labels)
        sel=1
      fi
      labels+="Andere …"
      hint="  Mit welchem Apple-Developer-Team signierst du?"
      (( ${#teams} == 0 )) && hint+=$'\n'"  ${YELLOW}Kein Zertifikat gefunden.${RESET} ${DIM}Xcode → Settings → Accounts → Apple-ID hinzufügen.${RESET}"
      menu 2 "Team" $sel "$hint" $labels
      if (( CHOICE <= ${#ids} )); then
        team=$ids[CHOICE]; step=3
      else
        prompt 2 "Team" "  Team-ID eingeben (10 Zeichen, steht auf developer.apple.com → Membership):"
        input=${(U)INPUT// /}
        if [[ -z $input ]]; then continue
        elif valid_team $input; then team=$input; step=3
        else screen 2 "Team"; print "  ${RED}„$INPUT“ ist keine gültige Team-ID.${RESET}"; wait_key
        fi
      fi ;;
    3)
      suggestion=$(suggested_prefix)
      prefixes=($prefix)
      [[ $suggestion != $prefix ]] && prefixes+=$suggestion
      labels=()
      for p in $prefixes; do labels+="$p ${DIM}→ $p.housephone${RESET}"; done
      labels+=("Andere …" "← Zurück")
      menu 3 "Bundle-ID-Präfix" 1 "  Die Bundle-IDs müssen in deinem Team eindeutig sein. Nimm dein eigenes
  Präfix, wenn du nicht com.jorisconrad bist." $labels
      if (( CHOICE <= ${#prefixes} )); then
        prefix=$prefixes[CHOICE]; step=4
      elif (( CHOICE == ${#prefixes} + 2 )); then
        step=2
      else
        prompt 3 "Bundle-ID-Präfix" "  Präfix in umgekehrter Domain-Schreibweise, z. B. de.meinname:"
        input=${INPUT// /}
        if [[ -z $input ]]; then continue
        elif valid_prefix $input; then prefix=$input; step=4
        else screen 3 "Bundle-ID-Präfix"; print "  ${RED}„$INPUT“ ist kein gültiges Präfix.${RESET}"; wait_key
        fi
      fi ;;
    4)
      summary="  Team          ${BOLD}$team${RESET}
  iPhone-App    ${BOLD}$prefix.housephone${RESET}
  Watch-App     ${BOLD}$prefix.housephone.watchkitapp${RESET}
  Local Push    ${BOLD}$prefix.housephone.localpush${RESET} ${DIM}(noch nicht eingebettet, siehe ${ADR_LOCAL_PUSH:t})${RESET}

  Bridge        apns.topic: ${BOLD}$prefix.housephone.voip${RESET}

  Wird gespeichert in ${DIM}$LOCAL_CONFIG${RESET}"
      menu 4 "Zusammenfassung" 1 "$summary" "Speichern" "← Zurück" "Abbrechen ohne Speichern"
      case $CHOICE in
        1) break ;;
        2) step=3 ;;
        3) finish 1 ;;
      esac ;;
  esac
done

write_local_config $team $prefix

done_text="  ${GREEN}✓${RESET} Gespeichert: ${DIM}$LOCAL_CONFIG${RESET}

  ${BOLD}So geht's weiter${RESET}
  1. In Xcode oben dein iPhone wählen, Schema ${BOLD}Housephone${RESET} → ▶︎
  2. Auf developer.apple.com bei ${BOLD}$prefix.housephone${RESET} und ${BOLD}…watchkitapp${RESET}
     „Push Notifications“ aktivieren.
  3. In der Bridge bzw. im Home-Assistant-Add-on eintragen:
     apns.topic / apns_topic = ${BOLD}$prefix.housephone.voip${RESET}
     apns.teamId / apns_team_id = ${BOLD}$team${RESET}"
menu 0 "Fertig" 1 "$done_text" "Projekt in Xcode öffnen" "Beenden"
if (( CHOICE == 1 )); then
  open $PROJECT
fi
finish 0
