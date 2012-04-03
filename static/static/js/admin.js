/* handle clearing default fields ... */
$(function() {
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
    $(".more-button").click(function() {
        $(".extras").slideToggle();
    });
    $(".post-title-input").blur(function() {
        var value = $(this).attr("value");
        value = value.replace(/[^\w]+/g, "-").toLowerCase()
        $("#slug").attr("value", value);
    });
    $(".published-toggle-button").click(function() {
        var $this = $(this);
        var input = $("#published");
        if (input.attr("value") == "1") {
            input.attr("value", "0");
            $this.find("i").removeClass("icon-check").addClass("icon-remove");
            $this.removeClass("published-1").addClass("published-0");
        } else {
            input.attr("value", "1");
            $this.find("i").removeClass("icon-remove").addClass("icon-check");
            $this.removeClass("published-0").addClass("published-1");
        }
    });
});

