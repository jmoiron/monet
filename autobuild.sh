#!/bin/bash

cur=`pwd`

inotifywait -mqr --timefmt '%d/%m/%y %H:%M' --format '%T %w %f' \
   -e modify ./ | while read date time dir file; do
    ext="${file##*.}"
    if [[ "$ext" = "go" ]]; then
        #kill %1
        echo "$file changed @ $time $date, rebuilding..."
        go build
        #./monet &
    fi
done

