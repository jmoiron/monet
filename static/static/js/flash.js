// $.flash(message) or $.flash(message, level)
// level: "error" | "warning" (default is success/info)
$.flash = function(message, level) {
    $('#flash-banner').remove();
    const banner = $('<div id="flash-banner"></div>');
    banner.text(message);
    if (level) banner.addClass(level);
    $('body').append(banner);
    setTimeout(() => banner.addClass('fading'), 100);
};
