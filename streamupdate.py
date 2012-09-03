#!/usr/bin/env python
# -*- coding: utf-8 -*-

"""Stream loading support.  This is adapted from an old django app, and uses
various custom libraries to simplify API access:
    pip install https://github.com/jmoiron/python-github/tarball/master
    pip install https://bitbucket.org/jmoiron/python-bitbucket/get/tip.tar.gz
    pip install twitter-text-py argot lxml
"""

import sys
import os
import datetime
import time
import urllib2
import json
from pprint import pprint, pformat
from hashlib import md5
import pymongo

md5sum = lambda x: md5(x).hexdigest()
# maximum time to put something in cache
cachemax = 2591999

tmp = "/dev/shm/"
moddir = os.path.dirname(__file__)
config = json.load(open(os.path.join(moddir, "config.json")))

host = config.get("DbHost", "localhost")
port = config.get("DbPort", 27017)
con = pymongo.Connection(host, port)
db = con.monet

def touch(path, times=None):
    with file(path, 'a'):
        os.utime(path, times)

class Stream(object):
    def __init__(self, config):
        self.id = md5sum(repr(config))[:12]
        self.type = config.pop("type")
        self.interval = config.pop("interval", 900)
        self.path = os.path.join(tmp, "%s-%s" % (self.type, self.id))
        self.arguments = dict(config)

    def last_updated(self):
        try: stat = os.stat(self.path)
        except: return 0
        return stat.st_mtime

    def update(self, force=False):
        now = time.time()
        if force or (self.last_updated() + self.interval < now):
            print "Updating %s (%r)" % (self.type, self.arguments)
            if self.type == "twitter":
                TwitterStream().update(**self.arguments)
            elif self.type == "github":
                GithubStream().update(**self.arguments)
            touch(self.path)

    def __repr__(self):
        return "<Stream: %s:%s>" % (self.type, self.id)

def update(streams, force=False):
    for stream in streams:
        stream.update(force)

def rerender(streams):
    for stream in streams:
        stream.rerender()

def parse_args():
    import optparse
    parser = optparse.OptionParser(usage="./prog [options]")
    parser.add_option("-r", "--rerender", action="store_true")
    parser.add_option("-f", "--force", action="store_true")
    # fix an old github bug
    parser.add_option("", "--github-fix-1", action="store_true", help="fix an issue with github urls")
    return parser.parse_args()

def main():
    opts, args = parse_args()

    if opts.github_fix_1:
        github_fix_1()
        return

    streams = map(Stream, config["Streams"])
    if opts.rerender:
        rerender(streams)
        return
    update(streams, opts.force)

class TwitterStream(object):
    url = "http://api.twitter.com/1/statuses/user_timeline.json"

    def get_tweets(self, user_id, count=20):
        url = self.url + '?user_id=%s&count=%s' % (user_id, count)
        try:
            tweets = json.loads(urllib2.urlopen(url).read())
        except:
            tweets = []
        return tweets

    def update(self, all=False, **kw):
        import twitter_text
        user_id = kw["user_id"]
        tweets = self.get_tweets(user_id, 200 if all else 20)
        timeformat = '%a %b %d %H:%M:%S +0000 %Y'
        for tweet in tweets:
            checksum = md5sum(tweet['text'].encode("utf-8"))
            if db.stream.find({"checksum":checksum}).count():
                continue
            sourceid = str(tweet["id"])
            entry = db.stream.find_one({"sourceid": sourceid})
            if not entry:
                entry = {"type":"twitter", "sourceid": sourceid}
            entry["checksum"] = checksum

            user = tweet['user']
            tweet['html_text'] = twitter_text.TwitterText(tweet['text']).autolink.auto_link()

            entry["title"] = "tweet @ %s" % tweet['created_at']
            entry["timestamp"] = int(time.mktime(time.strptime(tweet["created_at"], timeformat)))
            entry["url"] = 'http://twitter.com/%s/status/%s' % (user['screen_name'], tweet['id_str'])
            entry["data"] = json.dumps({"tweet":tweet})
            db.stream.save(entry)

class GithubStream(object):

    def get_repos(self, username, all=False):
        user = self.handle.user(username)
        repos = user.repositories(all=all)
        for repo in repos:
            repo[u'project'] = '%s/%s' % (repo['owner'], repo['name'])
            repo[u'event'] = 'fork' if repo['fork'] else 'create'
            repo[u'id'] = '%s-%s-%s' % (repo['event'], repo['owner'], repo['name'])
        return repos

    def get_commits(self, username, repos, all=False):
        user = self.handle.user(username)
        all_commits = []
        for repo in repos:
            commits = user.repository(repo['name']).commits(all=all)
            for commit in commits:
                commit['repository'] = repo
                commit['event'] = 'commit'
            commits = [c for c in commits if c['committer'] and c['committer']['login'] == user.username]
            all_commits += commits
        return all_commits

    def update(self, all=False, **args):
        from github import Github, to_datetime
        from argot import utils
        username = args["username"]
        self.handle = Github(username=username)

        repos = self.get_repos(username, all=all)
        commits = self.get_commits(username, repos, all=all)

        for repo in repos:
            sourceid = str(repo["id"])
            if db.stream.find({"type":"github", "sourceid":sourceid}).count():
                continue

            timestamp = int(time.mktime(to_datetime(repo["created_at"]).timetuple()))
            entry = {
                "sourceid": sourceid,
                "type": "github",
                "timestamp": timestamp,
                "title": "%s %sed @ %s" % (repo["name"], repo["event"], timestamp),
                "url": repo["url"],
                "data": json.dumps({"event":repo}),
            }
            db.stream.save(entry)

        for commit in commits:
            sourceid = str(commit["sha"])
            if db.stream.find({"sourceid": sourceid}).count():
                continue

            entry = {"type":"github", "sourceid":sourceid}
            timestamp = int(time.mktime(to_datetime(commit["commit"]["author"]["date"]).timetuple()))
            # This might block for some time:
            details = self.handle.repository(username, commit['repository']['name']).commit(commit["sha"])
            commit.update(details)
            for mod in commit.get('modified', []):
                mod['htmldiff'] = utils.pygmentize(mod['diff'], 'diff', cssclass="diff")

            entry["timestamp"] = timestamp
            entry["title"] = "committed %s to %s" % (commit["sha"], commit['repository']['name'])
            if "message" not in commit:
                commit["message"] = commit["commit"]["message"]
            if commit['url'].startswith("https://api.github.com/repos"):
                commit["url"] = commit["url"].replace("https://api.github.com/repos", "")

            entry["url"] = ("https://github.com%s" % commit["url"]).replace("/commits/", "/commit/")
            entry["data"] = json.dumps({'event' : commit})
            db.stream.save(entry)

def github_fix_1():
    # fix urls
    import github
    fixed = 0
    double_urls = db.stream.find({"type": "github", "url": {"$regex": "https://github.comhttp.*", "$options": "i"}})
    for entry in double_urls:
        entry["url"] = entry["url"].replace("github.comhttps", "")
        db.stream.save(entry)
        fixed += 1

    double_semi_urls = db.stream.find({"type": "github", "url": {"$regex": "https://://.*"}})
    for entry in double_semi_urls:
        entry["url"] = entry["url"].replace("://://", "://")
        db.stream.save(entry)
        fixed += 1

    api_urls = db.stream.find({"type": "github", "url": {"$regex": "https://api.github.*", "$options": "i"}})
    for entry in api_urls:
        entry["url"] = "http://github.com%s" % (entry["url"].replace("https://api.github.com/repos", ""))
        db.stream.save(entry)
        fixed += 1

    commits_urls = db.stream.find({"type":"github", "url": {"$regex": ".*/commits/.*"}})
    for entry in commits_urls:
        entry["url"] = entry["url"].replace("/commits/", "/commit/")
        db.stream.save(entry)
        fixed += 1

    # fix messages that are commits but do not have a message in the commit data
    entries = db.stream.find({"type": "github"})
    for entry in entries:
        data = json.loads(entry["data"])
        commit = data["event"]
        if "message" not in commit and "commit" in commit:
            commit["message"] = commit["commit"]["message"]
            entry["data"] = json.dumps({"event": commit})
            db.stream.save(entry)
            fixed += 1

    # fix timestamps on creates and forks
    entries = db.stream.find({"type": "github"})
    for entry in entries:
        data = json.loads(entry["data"])
        event = data["event"]
        if event["event"] != "commit":
            entry["timestamp"] = int(time.mktime(github.to_datetime(event["created_at"]).timetuple()))
            db.stream.save(entry)
            fixed += 1
        else:
            if "committed_date" in event:
                ts = event["committed_date"]
            else:
                # fix the absense of "committed_date" key in older events
                ts = event["commit"]["author"]["date"]
                event["committed_date"] = ts
                entry["data"] = json.dumps({"event": event})

            entry["timestamp"] = time.mktime(github.to_datetime(ts).timetuple())
            db.stream.save(entry)
            fixed += 1

    if fixed == 1:
        print "Fixed 1 entry"
    else:
        print "Fixed %d entries." % fixed


if __name__ == "__main__":
    try:
        ret = main()
    except KeyboardInterrupt:
        ret = 0
    sys.exit(ret)

'''
def get_detailed_updates(user, limit=50):
    """Gets the most recent `limit` updates for a bitbucket user object.
    This object is created by BitBucket().user(username)."""
    events = user.events(limit=50)['events']
    events = [e for e in events if e['event'] != 'commit']
    # filer out issue updates and comments since they have no data
    events = [e for e in events if not e['event'].startswith('issue_')]
    for e in events:
        e['created_on'] = to_datetime(e['created_on'])
    repos = user.repositories()
    checkins = []
    for repo in repos:
        repo['url'] = "http://bitbucket.org/%s/%s/" % (user.username, repo['slug'])
        # this can be empty for repos that have not had a changeset
        changesets = user.repository(repo['slug']).changesets()
        if 'changesets' in changesets:
            changesets = changesets['changesets']
        for cs in changesets:
            cs['repository'] = repo
        checkins += changesets
    for c in checkins:
        c['created_on'] = to_datetime(c['timestamp'])
        c['event'] = 'commit'
        c['description'] = c['message']
    ret = events + checkins
    ret.sort(key=lambda item: item['created_on'], reverse=True)
    return ret[:limit] if limit > 0 else ret

class BitbucketPlugin(object):
    settings = stream_settings.get('bitbucket', {})
    tag = 'bitbucket'

    def get_api_handle(self):
        username = self.settings.get('username', None)
        password = self.settings.get('password', None)
        if username and password:
            return BitBucket(username, password)
        return BitBucket()

    def reprocess(self):
        """Batch re-processes all available StreamEntrys with this plugins tag."""
        for entry in StreamEntry.objects.filter(source_tag=self.tag):
            update = entry.data['update']
            entry.title = self.make_title(update)
            entry.permalink = self.make_url(update)
            entry.save()

    def make_title(self, update):
        if update['event'] == 'commit':
            return "commit #%d to <a href=\"%s\" title=\"%s\">%s</a>" % (update['revision'],
                update['repository']['url'],
                update['repository']['description'].replace('"', '&quot;'),
                update['repository']['name'])
        return "%s event" % (update['event'])

    def make_url(self, update):
        if update['event'] == 'commit':
            return "%schangeset/%s" % (update['repository']['url'], update['node'])
        return "FIXME"

    def force(self, all=False):
        """Updates the events on the user set in settings."""
        bb = self.get_api_handle()
        user = bb.user(self.arguments['username'])
        limit = 0 if all else 20
        updates = get_detailed_updates(user, limit=limit)
        for update in updates:
            if update['node'] is None:
                update['node'] = "%s-%s" % (update['event'], update['repository']['slug'])
            checksum = md5sum(repr(update))
            if self.in_cache(update['node'], checksum):
                continue
            try:
                entry = StreamEntry.objects.get(source_tag=self.tag, source_id=update['node'])
            except StreamEntry.DoesNotExist:
                entry = StreamEntry(source_tag=self.tag,
                    source_id=str(update['node']),
                    plugin=self.stream_plugin)

            if entry.md5sum == checksum:
                self.set(update['node'], checksum)
                continue

            entry.title = self.make_title(update)
            entry.timestamp = update['created_on']
            entry.permalink = self.make_url(update)
            entry.data = {
                'update' : update
            }
            entry.md5sum = checksum
            entry.save()
        self.stream_plugin.last_run = datetime.datetime.now()
        self.stream_plugin.save()
'''
