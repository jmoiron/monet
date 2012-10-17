#!/bin/bash

cur=`pwd`
kill=1

inotifywait -mqr --timefmt '%d/%m/%y %H:%M' --format '%T %w %f' \
   -e modify ./ | while read date time dir file; do
    ext="${file##*.}"
    if [[ "$ext" = "go" ]]; then
        if [ $kill -eq 0 ]; then
            kill %1
        fi
        echo "$file changed @ $time $date, rebuilding..."
        go build
        if [ $? -eq 0 ]; then
            kill=0
            sleep 1
            ./monet &
        else
            kill=1
        fi
    fi
done

