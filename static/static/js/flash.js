// $.flash(message) or $.flash(message, level)
// level: "error" | "warning" (default is success/info)
$.flash = function(message, level) {
    $('#flash-banner').remove();
    const banner = $('<div id="flash-banner"></div>');
    banner.text(message);
    if (level) {
        banner.addClass(level);
        // problems stay up long enough to be read
        const ms = Math.min(12000, Math.max(4000, 3000 + 50 * message.length));
        banner.css('transition-duration', (ms / 1000) + 's');
    }
    $('body').append(banner);
    setTimeout(() => banner.addClass('fading'), 100);
};

// $.flashNext(message, level) shows the flash after the next page load, for
// when the page is about to reload and would wipe a flash shown now.
$.flashNext = function(message, level) {
    try {
        sessionStorage.setItem('flash-next', JSON.stringify({message: message, level: level}));
    } catch (e) {}
};

$.flashPending = function() {
    try {
        const raw = sessionStorage.getItem('flash-next');
        if (!raw) return;
        sessionStorage.removeItem('flash-next');
        const f = JSON.parse(raw);
        $.flash(f.message, f.level);
    } catch (e) {}
};

$(() => $.flashPending());
