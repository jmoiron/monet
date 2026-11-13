// Photos in the gallery admin open in a modal over the page they were
// chosen from, rather than a page of their own. Any link to a photo's edit page
// does it, and the modal goes away with Escape, a click outside it, or its X.
(function() {
    var editURL = /^\/admin\/gallery\/edit\/[^\/?#]+$/;
    var $overlay, $body, $link;

    function close() {
        if ($overlay) { $overlay.remove(); $overlay = null; }
        $(document).off('keydown.photomodal');
    }

    function bindForm(id) {
        var $form = $body.find('form.photo-form');
        $form.find('.published-toggle-button').on('click', function() {
            var $input = $form.find('#published'), on = $input.val() === '1';
            $input.val(on ? '0' : '1');
            $(this).find('i').toggleClass('icon-check', !on).toggleClass('icon-remove', on);
            $(this).toggleClass('published-1', !on).toggleClass('published-0', on);
        });
        $form.on('submit', function(e) {
            e.preventDefault();
            var published = $form.find('#published').val();
            fetch($form.attr('action') || ('/admin/gallery/edit/' + id + '?modal=1'), {method: 'POST', body: new URLSearchParams(new FormData($form[0]))})
                .then(function(r) { if (!r.ok) { throw new Error(r.statusText); } return r.text(); })
                .then(function(html) {
                    $body.html(html);
                    bindForm(id);
                    // keep the tile behind the modal honest
                    if ($link) { $link.closest('.photo-tile').toggleClass('unpublished', published !== '1'); }
                })
                .catch(function(err) { $.flash('Could not save: ' + err.message, 'error'); });
        });
    }

    function open(href, $from) {
        var id = href.split('/').pop();
        close();
        $link = $from;
        $overlay = $('<div class="rename-modal photo-modal"><div class="modal-content">' +
            '<a href="#" class="modal-close" title="Close"><i class="fa-solid fa-circle-xmark"></i></a>' +
            '<div class="photo-modal-body">Loading…</div></div></div>').css('display', 'block').appendTo('body');
        $body = $overlay.find('.photo-modal-body');
        $overlay.on('mousedown', function(e) {
            if (e.target === this) { close(); }
        });
        $overlay.find('.modal-close').on('click', function(e) { e.preventDefault(); close(); });
        $(document).on('keydown.photomodal', function(e) { if (e.key === 'Escape') { close(); } });
        fetch(href + '?modal=1')
            .then(function(r) { if (!r.ok) { throw new Error(r.statusText); } return r.text(); })
            .then(function(html) { $body.html(html); bindForm(id); })
            .catch(function(err) { $body.text('Could not load the photo: ' + err.message); });
    }

    $(document).on('click', 'a[href]', function(e) {
        if (e.which > 1 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) { return; }
        var href = this.getAttribute('href');
        if (!editURL.test(href)) { return; }
        e.preventDefault();
        open(href, $(this));
    });
})();
