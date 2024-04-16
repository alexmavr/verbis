#!/bin/bash

APP_PATH="macapp/out/Verbis-darwin-arm64/Verbis.app"
ZIP_PATH="Verbis.zip"
BUNDLE_ID="ai.verbis.Verbis"
APPLE_ID=
PASSWORD=
TEAM_ID=

# Sign all necessary binaries
find "$APP_PATH" -type f \( -name "*.dylib" -o -name "*.so" -o -name "Verbis Helper*" \) -exec codesign --options runtime --timestamp --deep --force --verbose --sign "" {} \;

# Create ZIP file
ditto -c -k --sequesterRsrc --keepParent "$APP_PATH" "$ZIP_PATH"

# Submit for notarization
xcrun notarytool submit "$ZIP_PATH" --apple-id "$APPLE_ID" --password "$PASSWORD" --team-id "$TEAM_ID" --wait

# Staple the notarization ticket to the app
xcrun stapler staple "$APP_PATH"

# Verify the stapling
xcrun stapler validate "$APP_PATH"
