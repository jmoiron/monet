## Contents

This directory has the free version of FontAwesome 7 which is distributed under the licenses on this page:

https://fontawesome.com/license/free

The OFL expressly allows modifications of OFL-licenced fonts here:

https://openfontlicense.org/

The directory contains a tool, `minimize.py`, which uses a "spec file" to produce minimal CSS & woff2 output. The spec is a newline separated list of font-awesome classes. The resulting files can be used as in lieu of the full FA woff & CSS, which are several hundred kilobytes.

This tool was developed almost entirely by the LLM based tool claude code as an autodidact exercise in how to employ it to create such tools. The initial CLAUDE.md file is available in the git history; the current form is one that was updated by Claude itself. The chat logs are split over two log files, claude-log.txt and claude-log-2.txt, as development occured across two separate machines.

### Results

Overall, development of this tool took approximately 4-5 hours, including research on what libraries might be available to perform the font minimalization and then some manually testing to ensure that the CSS produced was actually usable in a browser.