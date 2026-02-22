function escapeHtml(text) {
    const map = {
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#039;'
    };
    return text.replace(/[&<>"']/g, m => map[m]);
}

function getTimeAgo(date) {
    const seconds = Math.floor((new Date() - date) / 1000);

    let interval = seconds / 31536000;
    if (interval > 1) return Math.floor(interval) + ' years ago';

    interval = seconds / 2592000;
    if (interval > 1) return Math.floor(interval) + ' months ago';

    interval = seconds / 86400;
    if (interval > 1) return Math.floor(interval) + ' days ago';

    interval = seconds / 3600;
    if (interval > 1) return Math.floor(interval) + ' hours ago';

    interval = seconds / 60;
    if (interval > 1) return Math.floor(interval) + ' minutes ago';

    return Math.floor(seconds) + ' seconds ago';
}

class Countdown {
    constructor(seconds) {
        this.seconds = seconds;
        this.remaining = 0;
        this._timer = null;
        this._interval = null;
    }

    onStart(fn)    { this._onStart = fn;    return this; }
    onTick(fn)     { this._onTick = fn;     return this; }
    onCancel(fn)   { this._onCancel = fn;   return this; }
    onComplete(fn) { this._onComplete = fn; return this; }

    start() {
        if (this._timer !== null) return this;
        this.remaining = this.seconds;
        if (this._onStart) this._onStart(this.remaining);
        this._interval = setInterval(() => this.tick(), 1000);
        this._timer = setTimeout(() => {
            this._clearTimers();
            if (this._onComplete) this._onComplete();
        }, this.seconds * 1000);
        return this;
    }

    tick() {
        this.remaining = Math.max(0, this.remaining - 1);
        if (this._onTick) this._onTick(this.remaining);
        return this;
    }

    cancel() {
        this._clearTimers();
        if (this._onCancel) this._onCancel();
        return this;
    }

    _clearTimers() {
        clearTimeout(this._timer);
        clearInterval(this._interval);
        this._timer = this._interval = null;
    }
}

// $.fn.autosave(options)
// Watches the matched inputs for changes, then POSTs their values to a URL
// after a countdown delay.
//
// Options:
//   url        {string}   required  endpoint to POST form data to
//   delay      {number}   optional  countdown seconds (default: 300)
//   countdown  {string}   optional  selector for the countdown display element
//   onSuccess  {function} optional  called with response data on successful save
//   onError    {function} optional  called with error on failed save
//
// The controller object is stored on each input via .data('autosave') and exposes:
//   save()       perform save if any input has changed
//   forceSave()  cancel countdown and save unconditionally
//   cancel()     cancel the countdown
$.fn.autosave = function(options) {
    const opts = Object.assign({ delay: 5 * 60 }, options);
    const inputs = this;
    let changed = false;

    const cd = new Countdown(opts.delay)
        .onTick(remaining => {
            if (!opts.countdown) return;
            const mins = Math.floor(remaining / 60);
            const secs = remaining % 60;
            $(opts.countdown).text(` ${mins}:${secs.toString().padStart(2, '0')}`);
        })
        .onCancel(() => {
            if (opts.countdown) $(opts.countdown).text('');
        })
        .onComplete(() => perform(false));

    inputs.on('input', () => {
        changed = true;
        cd.start();
    });

    function perform(force) {
        if (!force && !changed) return;

        const formData = new FormData();
        inputs.each(function() {
            const name = $(this).attr('name');
            if (name) formData.append(name, $(this).val());
        });

        $.flash('Autosaving...');

        fetch(opts.url, { method: 'POST', body: formData })
            .then(r => r.json())
            .then(data => {
                if (data.success) {
                    changed = false;
                    $.flash('Autosaved');
                    if (opts.onSuccess) opts.onSuccess(data);
                } else {
                    $.flash('Autosave failed', 'error');
                    if (opts.onError) opts.onError(data);
                }
            })
            .catch(err => {
                console.error('Autosave error:', err);
                $.flash('Autosave failed', 'error');
                if (opts.onError) opts.onError(err);
            });
    }

    const controller = {
        save:      () => perform(false),
        forceSave: () => { cd.cancel(); perform(true); },
        cancel:    () => cd.cancel(),
    };

    inputs.each(function() { $(this).data('autosave', controller); });
    return this;
};

$.fn.center = function() {
    var $window = $(window);
    this.css("top", (($window.height() - this.outerHeight())/2) + "px");
    this.css("left", (($window.width() - this.outerWidth())/2) + "px");
    return this;
}

$(() => {
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
      $(grid).find("#content-rendered").append(`<div class="loader-container"><span class="loader"></span></div>`);
      $(grid).find("#content-input").append($this);

      // run split to get resizable content
      window.Split({
          columnGutters: [{
              track: 1,
              element: document.querySelector('.gutter-col-1'),
          }],
      });

      $this.markdown($("#content-rendered"));
  };
});


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

    $(".shrink").on("click", () => {
      let current = $(".container").width();
      if (current > 1200) {
        $(".container").css("width", current-100);
      }
    });

    $(".grow").on("click", () => {
      let current = $(".container").width();
      if (current < 2400) {
        $(".container").css("width", current+100);
      }
    });

    $("#take-snapshot").on("click", (e) => {
        // swap the camera out with a small spinner
        // load the image
        // show the camera green if success, red if fail
        var $this = $(e.target);
        var id = $("#id").val();
        var container = $this.parent();
        var spinner = $(`<span class="loader-small"></span>`);
        console.log($this);
        container.html("");
        // container.remove($this);
        container.append(spinner);
        console.log(container);

        // Make fetch call to screenshot API
        fetch(`/admin/bookmarks/ss/${id}`)
            .then(response => response.json())
            .then(data => {
                console.log("Screenshot response:", data);
                if (data.success) {
                    // Update title if provided
                    if (data.title && data.title.length > 0) {
                        $("#title").val(data.title);
                    }
                    // Update description if provided
                    if (data.description && data.description.length > 0) {
                        $("#description").val(data.description);
                    }
                    
                    // Refresh the page to show the screenshot and updated fields
                    window.location.reload();
                } else {
                    // Show error and restore camera icon
                    console.error("Screenshot failed:", data.error);
                    container.html("");
                    container.append($this);
                }
            })
            .catch(error => {
                console.error("Screenshot failed:", error);
                // Restore the camera icon even on error
                container.html("");
                container.append($this);
            });
    });

    // Clear default values before form submission
    $("form").on("submit", function() {
        $(".js-clear-default", this).each(function() {
            var $this = $(this);
            var defaultValue = $this.attr("data-default");
            
            if ($this.is("textarea")) {
                if ($this.val() === defaultValue) {
                    $this.val("");
                }
            } else {
                if ($this.val() === defaultValue) {
                    $this.val("");
                }
            }
        });
    });

});
