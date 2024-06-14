#!/bin/bash

# Check if the correct number of arguments is provided
if [ "$#" -ne 2 ]; then
    echo "Usage: $0 <mode> <backup_id>"
    exit 1
fi

MODE=$1
BACKUP_ID=$2
USERNAME=$(whoami)
BACKUP_PATH="/Users/$USERNAME/.verbis/synced_data/backup"

if [ "$MODE" == "backup" ]; then
    echo "Starting backup with ID: $BACKUP_ID"

    # Create backup
    curl -X POST "http://localhost:8088/v1/backups/filesystem" \
    -H "Content-Type: application/json" \
    -d '{
      "id": "'"$BACKUP_ID"'",
      "include": ["ConnectorState", "Document", "VerbisChunk", "Conversation"]
    }'

    # Check if the backup was successful
    if [ $? -eq 0 ]; then
        # Copy backup config to restore config
        cp "$BACKUP_PATH/$BACKUP_ID/backup_config.json" "$BACKUP_PATH/$BACKUP_ID/restore_config.json"
    else
        echo "Backup failed."
        exit 1
    fi

elif [ "$MODE" == "restore" ]; then
    echo "Starting restore with ID: $BACKUP_ID"

    # Delete existing classes
    curl -X DELETE "http://localhost:8088/v1/schema/ConnectorState"
    curl -X DELETE "http://localhost:8088/v1/schema/Document"
    curl -X DELETE "http://localhost:8088/v1/schema/VerbisChunk"
    curl -X DELETE "http://localhost:8088/v1/schema/Conversation"

    # Check if the class deletions were successful
    if [ $? -ne 0 ]; then
        echo "Failed to delete existing classes."
        exit 1
    fi

    # Restore backup
    curl -X POST "http://localhost:8088/v1/backups/filesystem/$BACKUP_ID/restore" \
    -H "Content-Type: application/json" \
    -d '{
      "id": "'"$BACKUP_ID"'",
      "path": "'"$BACKUP_PATH/$BACKUP_ID"'"
    }'

    # Check if the restore was successful
    if [ $? -eq 0 ]; then
        echo "Restore completed successfully."
    else
        echo "Restore failed."
        exit 1
    fi
else
    echo "Invalid mode. Use 'backup' or 'restore'."
    exit 1
fi

