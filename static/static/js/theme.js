/* light/dark: the attribute is what the stylesheet looks at, localStorage what is remembered */
document.addEventListener("click", function(e) {
    if (!e.target.closest(".theme-toggle")) { return; }
    var root = document.documentElement;
    var next = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
    root.setAttribute("data-theme", next);
    try { localStorage.setItem("theme", next); } catch (err) {}
});
