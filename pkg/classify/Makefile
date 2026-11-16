# The python side of classify: a virtualenv with PyTorch (the right build for
# this machine) and what the workers in scripts/ need.
.PHONY: venv venv-plan sample

venv:
	python3 scripts/setup_venv.py

# what `make venv` would do, without doing it
venv-plan:
	python3 scripts/setup_venv.py --dry-run

# the photos of an instagram export, to try the classifiers on: make sample ZIP=path/to.zip
sample:
	mkdir -p sample/instagram
	unzip -q -o $(ZIP) 'media/*' -x '*.mp4' -d sample/instagram
