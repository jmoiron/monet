(() => {
  function buttonsMatching(re) {
    return [...document.querySelectorAll('button')].filter((b) => {
      return b.textContent.search(re) >= 0;
    })
  }

  // try to click cookie banners
  var buttons = buttonsMatching(/accept/gi);
  buttons.forEach((b) => {
    b.click();
  });

  return buttons.length;
})();