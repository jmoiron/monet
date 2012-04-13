$.fn.center = function() {
    var $window = $(window);
    this.css("top", (($window.height() - this.outerHeight())/2) + "px");
    this.css("left", (($window.width() - this.outerWidth())/2) + "px");
    return this;
}

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
    $(".more-button").click(function() {
        $(".extras").slideToggle();
    });
    $(".post-title-input").blur(function() {
        var value = $(this).attr("value");
        value = value.replace(/[^\w\s]/g, "");
        value = value.replace(/[^\w]+/g, "-").toLowerCase()
        $("#slug").attr("value", value);
    });
    /* ensure the slug is populated on load if it's empty */
    $("#slug").each(function() {
        if (!$(this).attr("value").length) {
            $(".post-title-input").blur();
        }
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
    $(".preview-button").click(function() {
        var overlay = $("#overlay");
        var form = $(this).parents("form")
        if (overlay.length == 0) {
            overlay = $("<div id=\"overlay\"></div>");
            overlay.appendTo(document.body)
        }
        $.ajax({
            type: "POST",
            url: $(this).attr("data-url"),
            data: form.serialize(),
            success: function(data, status, xhr) {
                var preview = $("<div id=\"preview-box\"></div>");
                preview.appendTo(document.body);
                preview.html(data);
                overlay.fadeIn();
                preview.center().fadeIn();
                overlay.click(function() { overlay.fadeOut(); preview.fadeOut(); });
            },
            error: function(xhr, status, err) {

            }
        });
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



