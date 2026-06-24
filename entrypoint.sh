#!/bin/sh

DB_PATH="${HYPANEL_DB_FOLDER:-/app/db}/hypanel.db"
if [ -f "$DB_PATH" ]; then
	./hypanel migrate
fi

exec ./hypanel