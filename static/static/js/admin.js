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
// Returns a controller object with:
//   save()       perform save if any input has changed
//   forceSave()  save unconditionally; cancels countdown on success
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
                    if (force) cd.cancel();
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

    return {
        save:      () => perform(false),
        forceSave: () => perform(true),
        cancel:    () => cd.cancel(),
    };
};

// $.fn.autosaveViewer(options)
// Creates and manages the autosave viewer modal. Call on the trigger button element.
//
// Options:
//   count        {number}   initial autosave count
//   listUrl      {string}   GET - returns autosave list with diffs
//   deleteUrl    {function} (id) => string - DELETE endpoint
//   autoclearUrl {string}   POST - deletes autosaves matching saved content
//   restoreUrl   {function} (id) => string - POST endpoint
//
// Returns { onNewSave() } to be called when a new autosave is created.
$.fn.autosaveViewer = function(options) {
    const trigger = this;
    let count = options.count || 0;
    let currentAutosaves = [];
    let selectedAutosave = null;

    const modal = $(`
        <div class="autosave-modal">
            <div class="autosave-modal-content">
                <div class="autosave-modal-header">
                    <h3>Autosaved Versions</h3>
                    <button type="button" class="autoclear-btn">Auto-clear</button>
                    <button type="button" class="autosave-modal-close">&times;</button>
                </div>
                <div class="autosave-modal-body">
                    <div class="autosave-list"></div>
                    <div class="autosave-diff" style="display:none;">
                        <div class="autosave-diff-header">
                            <button type="button" class="back-to-list-btn">&larr; Back to list</button>
                            <button type="button" class="restore-btn restore-button">Restore this version</button>
                        </div>
                        <div class="diff-content"></div>
                    </div>
                </div>
            </div>
        </div>
    `);
    $('body').append(modal);

    function open() {
        loadAutosaves();
        modal.show();
    }

    function close() {
        modal.hide();
    }

    function backToList() {
        modal.find('.autosave-diff').hide();
        modal.find('.autosave-list').show();
        selectedAutosave = null;
    }

    // Trigger: open modal
    trigger.on('click', (e) => {
        e.preventDefault();
        if (trigger.hasClass('inactive')) return;
        open();
    });

    // Close button and outside click
    modal.find('.autosave-modal-close').on('click', close);
    modal.on('click', (e) => { if (e.target === modal[0]) close(); });

    // Back to list
    modal.find('.back-to-list-btn').on('click', backToList);

    // Esc: back to list from diff, or close from list
    $(document).on('keydown', (e) => {
        if (e.key !== 'Escape' || modal.css('display') === 'none') return;
        selectedAutosave ? backToList() : close();
    });

    // Auto-clear: reload page with modal open on success
    modal.find('.autoclear-btn').on('click', () => {
        fetch(options.autoclearUrl, { method: 'POST' })
            .then(r => r.json())
            .then(data => {
                if (data.success) {
                    const url = new URL(window.location.href);
                    url.searchParams.set('autosave_modal', '1');
                    window.location.href = url.toString();
                } else {
                    $.flash('Failed to auto-clear autosaves', 'error');
                }
            })
            .catch(() => $.flash('Failed to auto-clear autosaves', 'error'));
    });

    // Restore
    modal.find('.restore-btn').on('click', () => {
        if (!selectedAutosave) return;
        if (!confirm('Restore this version? Your current unsaved changes will be lost.')) return;
        fetch(options.restoreUrl(selectedAutosave.id), { method: 'POST' })
            .then(r => r.json())
            .then(data => {
                if (data.success) location.reload();
                else $.flash('Failed to restore autosave', 'error');
            })
            .catch(() => $.flash('Failed to restore autosave', 'error'));
    });

    function loadAutosaves() {
        modal.find('.autosave-list').html('<p>Loading autosaves...</p>');
        modal.find('.autosave-diff').hide();
        modal.find('.autosave-list').show();
        fetch(options.listUrl)
            .then(r => r.json())
            .then(autosaves => {
                currentAutosaves = autosaves;
                if (autosaves.length === 0) {
                    modal.find('.autosave-list').html('<p>No autosaves found.</p>');
                } else {
                    displayList(autosaves);
                }
            })
            .catch(() => modal.find('.autosave-list').html('<p>Failed to load autosaves.</p>'));
    }

    function displayList(autosaves) {
        let html = '<div class="autosave-items">';
        autosaves.forEach(a => {
            html += `
                <div class="autosave-item" data-autosave-id="${a.id}">
                    <div class="autosave-info" data-autosave-id="${a.id}">
                        <div class="autosave-time">${getTimeAgo(new Date(a.created_at))}</div>
                        <div class="autosave-preview">${escapeHtml(a.title || 'Untitled')}</div>
                    </div>
                    <a class="del delete-autosave-btn" href="#" data-autosave-id="${a.id}">
                        <i class="fa-solid fa-circle-xmark"></i>
                    </a>
                </div>`;
        });
        html += '</div>';
        modal.find('.autosave-list').html(html);

        modal.find('.autosave-info').on('click', function() {
            viewDiff(parseInt($(this).attr('data-autosave-id')));
        });
        modal.find('.delete-autosave-btn').on('click', function(e) {
            e.preventDefault();
            deleteAutosave(parseInt($(this).attr('data-autosave-id')));
        });
    }

    function deleteAutosave(autosaveId) {
        fetch(options.deleteUrl(autosaveId), { method: 'DELETE' })
            .then(r => r.json())
            .then(data => {
                if (data.success) {
                    currentAutosaves = currentAutosaves.filter(a => a.id !== autosaveId);
                    modal.find(`.autosave-item[data-autosave-id="${autosaveId}"]`).remove();
                    count = Math.max(0, count - 1);
                    if (count === 0) {
                        trigger.addClass('inactive');
                        trigger.find('.autosave-count').text('');
                        modal.find('.autosave-list').html('<p>No autosaves found.</p>');
                    } else {
                        trigger.find('.autosave-count').text(` [${count}]`);
                    }
                } else {
                    $.flash('Failed to delete autosave', 'error');
                }
            })
            .catch(() => $.flash('Failed to delete autosave', 'error'));
    }

    function viewDiff(autosaveId) {
        const a = currentAutosaves.find(a => a.id === autosaveId);
        if (!a) return;
        selectedAutosave = a;
        modal.find('.diff-content').html(renderUnifiedDiff(a.diff));
        modal.find('.autosave-list').hide();
        modal.find('.autosave-diff').show();
    }

    function renderUnifiedDiff(diff) {
        if (!diff) return '<p>No differences from saved content.</p>';
        const lines = diff.split('\n');
        let html = '<div class="unified-diff">';
        for (const line of lines) {
            let cls = 'diff-context';
            if (line.startsWith('---') || line.startsWith('+++')) cls = 'diff-file-header';
            else if (line.startsWith('@@'))  cls = 'diff-hunk-header';
            else if (line.startsWith('-'))   cls = 'diff-removed';
            else if (line.startsWith('+'))   cls = 'diff-added';
            html += `<div class="${cls}">${escapeHtml(line) || ' '}</div>`;
        }
        html += '</div>';
        return html;
    }

    // Re-open modal after autoclear redirect
    if (new URLSearchParams(window.location.search).has('autosave_modal')) {
        const url = new URL(window.location.href);
        url.searchParams.delete('autosave_modal');
        history.replaceState(null, '', url.toString());
        open();
    }

    return {
        onNewSave() {
            count = Math.min(count + 1, 10);
            trigger.removeClass('inactive');
            trigger.find('.autosave-count').text(` [${count}]`);
        }
    };
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
