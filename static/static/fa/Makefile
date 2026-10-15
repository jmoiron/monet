VE := ve
PYTHON := $(VE)/bin/python

.PHONY: all clean

all: $(VE)/.installed
	$(PYTHON) minimize.py icons.spec --url /static/fa/icons.woff2

# bootstrap the virtualenv (fonttools + brotli for woff2) when missing or when
# requirements change
$(VE)/.installed: requirements.txt
	python3 -m venv $(VE)
	$(VE)/bin/pip install -q -r requirements.txt
	touch $@

clean:
	rm -f icons.css icons.woff2
