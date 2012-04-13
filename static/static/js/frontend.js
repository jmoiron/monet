
$(function() {
    /* handle clearing default fields ... */
    $(".js-clear-default").focus(function() {
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
    }).blur(function() {
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
            console.log("Hiya");
            var $this = $(this);
            $this.attr("href", $this.attr("href") + qs);
        });
    }
});
