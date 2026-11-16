# the GeoNames files the geo tool reads: cities of 1000, 5000 and 15000 people
# and over, and the names of countries and of their regions
.PHONY: data
data:
	mkdir -p data
	cd data && for f in cities1000.zip cities5000.zip cities15000.zip countryInfo.txt admin1CodesASCII.txt; do \
		curl -sSfO https://download.geonames.org/export/dump/$$f; done
