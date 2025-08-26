(() => {
  let matcher = /(accept|continue)/i;

  function buttonsMatching(re) {
    return [...document.querySelectorAll('button')].filter((b) => {
      return b.textContent.search(re) >= 0;
    })
  }

  // try to click cookie banners
  let buttons = buttonsMatching(matcher);
  buttons.forEach((b) => {
    b.click();
  });

  // if no buttons match, try with "a" elements
  let links = [...document.querySelectorAll('a')].filter(
      (b) => { return b.textContent.search(matcher) >= 0 }
  );
  if (links.length == 1) {
      links.forEach((b) => b.click());
  }

  return buttons.length + links.length;
})();