#!/bin/sh
# Lists the files in the images dir that the site refers to: links to /i/... or
# jmoiron.net/i/... in any text of the database (posts, pages, the stream,
# bookmarks, og images). Paths come out
# relative to the images dir, one a line, ready for rsync --files-from.
#
#   scripts/image-refs.sh monet.db ~/i > keep.txt          (on the old server)
#   rsync -a --files-from=keep.txt oldserver:i/ /opt/monet/images/
#
# Without the images dir it lists every reference; with it, only those that
# exist, and the ones that don't are named on stderr.
set -eu
db=${1:?usage: image-refs.sh monet.db [imagesdir]}
dir=${2:-}

{
	# every table's text, shadow tables of the full-text indexes included (so
	# some of what comes out is the same thing twice; sort -u deals with that).
	# A reference is /i/ at the start of a link, or after jmoiron.net: not
	# twitter.com/i/web/..., which is someone else's.
	sqlite3 "$db" .dump | perl -nE '
		while (m{(?:jmoiron\.net|(?<![\w.\-/]))/i/([^\s"\x27<>()\\\]\[]+)}g) {
			my $p = $1;
			$p =~ s/[?#].*//s;              # query and fragment
			$p =~ s/[.,;:!*]+$//;           # the end of a sentence
			$p =~ s/%([0-9A-Fa-f]{2})/chr hex $1/ge;
			next if $p =~ m{^(web|photos)/} || $p =~ m{(^|/)\.\.(/|$)} || $p eq "" || $p =~ m{/$};
			say $p;
		}'
} | sort -u | {
	if [ -z "$dir" ]; then cat; exit; fi
	while IFS= read -r f; do
		if [ -f "$dir/$f" ]; then echo "$f"; else echo "missing: $f" >&2; fi
	done
}
