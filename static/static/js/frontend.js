
$(function() {
    $(".js-clickable-row").on("click keydown", function(e) {
        var $target = $(e.target);
        if ($target.closest("a, button, input, textarea, select, summary, label").length) {
            return;
        }
        if (e.type === "keydown" && e.key !== "Enter" && e.key !== " ") {
            return;
        }
        if (e.type === "keydown") {
            e.preventDefault();
        }
        window.location = $(this).attr("data-href");
    });

    /* handle clearing default fields ... */
    $(".js-clear-default").on("focus", function() {
        var $this = $(this);
        if ($this.is("textarea")) {
            if ($this.html() == $this.attr("data-default")) {
                $this.html("");
            }
        } else {
            if ($this.attr("value") == $this.attr("data-default")) {
                $this.attr("value", "")
            }
        }
    }).on("blur", function() {
        var $this = $(this)
        if ($this.is("textarea")) {
            if (!$this.html()) {
                $this.html($this.attr("data-default"));
            }
        } else {
            if (!$this.attr("value")) {
                $this.attr("value", $this.attr("data-default"));
            }
        }
    });

    /* prettyprint */
    $("pre[lang]").each(function() {
        $(this).addClass("prettyprint");
        $(this).addClass("linenums");
    });
    if (window.location.search.length > 0) {
        var qs = window.location.search;
        $("ul.paginator a").each(function() {
            var $this = $(this);
            $this.attr("href", $this.attr("href") + qs);
        });
    }
});
