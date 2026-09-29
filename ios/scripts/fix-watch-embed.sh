#!/bin/sh
# Runs after `xcodegen generate` (postGenCommand).
#
# Since Xcode 16, a watchOS app must be embedded in the iPhone app's
# PlugIns/ directory; the legacy Watch/ directory makes installation on a
# device fail ("… must be embedded in the parent app bundle's PlugIns
# directory"). XcodeGen 2.46 still generates Watch/ — see
# https://github.com/yonaskolb/XcodeGen/issues/1613 (fix pending in #1614).
# Remove this script once an XcodeGen release embeds into PlugIns/.
set -eu

project="${1:-Housephone.xcodeproj}/project.pbxproj"

perl -0pi -e 's{(/\* Embed Watch Content \*/ = \{.*?)dstPath = "\$\(CONTENTS_FOLDER_PATH\)/Watch";(\s*)dstSubfolderSpec = 16;}{$1dstPath = "";$2dstSubfolderSpec = 13;}s' "$project"

if grep -q 'CONTENTS_FOLDER_PATH)/Watch' "$project"; then
    echo "fix-watch-embed: Watch/ embedding still present in $project" >&2
    exit 1
fi
