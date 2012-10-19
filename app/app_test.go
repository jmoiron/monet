package app

import (
	"testing"
)

func TestSlugify(t *testing.T) {

	m := map[string]string{
		"Simple Whitespace":                            "simple-whitespace",
		"Github has won":                               "github-has-won",
		"Virtualenv Tips":                              "virtualenv-tips",
		"Dancing with the devil":                       "dancing-with-the-devil",
		"OSX a month on":                               "osx-a-month-on",
		"Distracting Misconceptions":                   "distracting-misconceptions",
		"Fix the problem":                              "fix-the-problem",
		"Err 01 on Canon 500d w/ Tamron 17-50mm lens":  "err-01-on-canon-500d-w-tamron-17-50mm-lens",
		"Open Sources":                                 "open-sources",
		"Japanese peer-to-peer":                        "japanese-peer-to-peer",
		"Linux Software RAID-5: mdadm":                 "linux-software-raid-5-mdadm",
		"Miravi":                                       "miravi",
		"QuerySet Caching":                             "queryset-caching",
		"Deploying django on mod_wsgi, virtualenv":     "deploying-django-on-mod-wsgi-virtualenv",
		"Finding images in a binary file w/ python":    "finding-images-in-a-binary-file-w-python",
		"Wikis and the False Promise of Documentation": "wikis-and-the-false-promise-of-documentation",
		"Things I wish Google Chrome had":              "things-i-wish-google-chrome-had",
		"Tool philosophy":                              "tool-philosophy",
		"Cheesy color console output":                  "cheesy-color-console-output",
		"Man in Blacklists":                            "man-in-blacklists",
		"The joys of racing":                           "the-joys-of-racing",
		"Subclassing Django's TestCase":                "subclassing-djangos-testcase",
		"InnoDB transaction isolation":                 "innodb-transaction-isolation",
		"Profiling Generalizations":                    "profiling-generalizations",
		"Early impressions on sass":                    "early-impressions-on-sass",
		"It's in the game":                             "its-in-the-game",
		"Writing a simple HTTP scraper":                "writing-a-simple-http-scraper",
		"And Across the Line":                          "and-across-the-line",
		"FP Militancy":                                 "fp-militancy",
		"Darkening a color":                            "darkening-a-color",
		"Steve Jobs":                                   "steve-jobs",
		"Education and Entitlement":                    "education-and-entitlement",
		"Lifestream":                                   "lifestream",
		"iPhone wifi timeouts":                         "iphone-wifi-timeouts",
		"What is Node.js for?":                         "what-is-nodejs-for",
		"Linux Mint 12: Oneiric Revisited":             "linux-mint-12-oneiric-revisited",
		"Serving Fonts off AWS Cloudfront":             "serving-fonts-off-aws-cloudfront",
		"About SQLAlchemy and Django's ORM":            "about-sqlalchemy-and-djangos-orm",
		"Johnny Cache":                                 "johnny-cache",
		"Is johnny-cache for you?":                     "is-johnny-cache-for-you",
		"Totto Ramen":                                  "totto-ramen",
		"NodeJS, events, and closures":                 "nodejs-events-and-closures",
		"Zeroes and Ones":                              "zeroes-and-ones",
		"Oneiric First Impressions":                    "oneiric-first-impressions",
		"Python serialization":                         "python-serialization",
		"Async Hell: Gevent & Requests":                "async-hell-gevent-requests",
		"The Abyss Stares Back":                        "the-abyss-stares-back",
		"A Meek Defense of regex":                      "a-meek-defense-of-regex",
		"The Tooling Tarpit":                           "the-tooling-tarpit",
		"Template FizzBuzz":                            "template-fizzbuzz",
	}

	for in, out := range m {
		if Slugify(in) != out {
			t.Errorf("Expected \"%s\" -> \"%s\", got \"%s\"\n", in, out, Slugify(in))
		}
	}
}
