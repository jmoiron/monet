#!/bin/bash

cur=`pwd`
kill=1

function watchfile() {
    inotifywait -mqr --timefmt '%d/%m/%y %H:%M' --format '%T %w %f' \
       -e modify ./ | while read date time dir file; do
        ext="${file##*.}"
        if [[ "$ext" = "go" ]]; then
            echo "$file changed @ $time $date, rebuilding..."
            ./rebuild.sh
        fi
    done
}

case $(uname) in
    Darwin)
        if [ -z $(which watchmedo) ]; then
            echo "Please install watchdog:"
            echo "    pip install watchdog"
            exit
        fi
        watchmedo shell-command --patterns="*.go" --command="./rebuild.sh" .
    ;;
    *)
        watchfile
    ;;
esac


