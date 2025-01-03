$.fn.center = function() {
    var $window = $(window);
    this.css("top", (($window.height() - this.outerHeight())/2) + "px");
    this.css("left", (($window.width() - this.outerWidth())/2) + "px");
    return this;
}

$.fn.livePreview = function() {
    // adjust the width of the main container to better suit
    // having a live preview editing widget
    $(".container").css("width", "1200px");

    // grab the textarea element we're live-editing
    var $this = $(this);
    var grid = $(`<div class="grid content-edit-grid">
            <div id="content-input"></div>
            <div class="gutter-col gutter-col-1"></div>
            <div id="content-rendered"></div>
        <div>`);
    parent = $this.parent();
    parent.remove($this);
    parent.append(grid);
    $(grid).find("#content-input").append($this);

    // run split to get resizable content
    window.Split({
        columnGutters: [{
            track: 1,
            element: document.querySelector('.gutter-col-1'),
        }],
    });


};

$(function() {
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

    $(".more-button").on("click", function() {
        $(".extras").toggle();
    });

    $(".post-title-input").on("blur", function() {
        var value = $(this).attr("value");
        value = value.replace(/[^\w\s]/g, "");
        value = value.replace(/[^\w]+/g, "-").toLowerCase()
        $("#slug").attr("value", value);
    });

    /* ensure the slug is populated on load if it's empty */
    $("#slug").each(function() {
        if (!$(this).attr("value").length) {
            $(".post-title-input").trigger("blur");
        }
    });

    $(".published-toggle-button").on("click", function() {
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

    if (window.location.search.length > 0) {
        var qs = window.location.search;
        $("ul.paginator a").each(function() {
            var $this = $(this);
            $this.attr("href", $this.attr("href") + qs);
        });
    }

});
