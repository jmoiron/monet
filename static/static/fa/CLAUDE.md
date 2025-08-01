This directory contains 4 font files (*.woff2) and 4 CSS files. Some info about these files:

- the font files contain icons separated into 3 families: brands, regular, and solid
- the css files contain rules to set up these fonts as icons and then to display the fonts using css classes
- the css classes come in two forms:
  - classes like `fa-solid` that select the family
  - classes like `fa-circle-info` to select the icon

Additionally, I'm defining a "spec file" to be a text file that has pairs of css class rules like `fa-solid fa-circle-info`, one per line.

Write a tool called "minimize.py" in python3 that can produce a minimal font file and css file for a given spec file.
- the "minimal" font and css files contain only the rules required for the icons in the spec file and nothing more
- running the tool against "icons.spec" can output "icons.woff2" and "icons.css"
- each rule in the css file will contain a unicode character corresponding to the codepoint in the woff2 file
- use the fontTools python3 lib, including fontTools.subset, to create a subset font from the woff2 file that contains only the codes specified by the classes in the css file that are mentioned by the spec file
- produce a set of functions designed to create the minimal css rules;  they will need to copy the class definitions themselves as well as any supporting classes and supporting font declarations.
