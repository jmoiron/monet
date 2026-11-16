/* a paginator carries the query of the page it is on to its links, unless a link has a query of its own (a search's) */
$(function() {
    var qs = window.location.search;
    if (qs.length === 0) { return; }
    $("ul.paginator a").each(function() {
        var $this = $(this);
        if ($this.attr("href").indexOf("?") !== -1) { return; }
        $this.attr("href", $this.attr("href") + qs);
    });
});
