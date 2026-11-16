// A photo's page, over the page that links to it: a link with data-photo="ID"
// opens /gallery/ID as an overlay at #ID, and closing it goes back to the page
// as it was. /gallery/ID by itself is still the photo's page.
(function() {
    var LEAFLET = 'https://cdnjs.cloudflare.com/ajax/libs/leaflet/1.9.4/';
    var overlay = null, panel = null, current = null, opened = 0;

    function load(tag, attrs) {
        return new Promise(function(resolve, reject) {
            var el = document.createElement(tag);
            for (var k in attrs) { el[k] = attrs[k]; }
            el.onload = resolve; el.onerror = reject;
            document.head.appendChild(el);
        });
    }
    var leaflet = null;
    function loadLeaflet() {
        if (window.L) { return Promise.resolve(); }
        if (!leaflet) {
            load('link', {rel: 'stylesheet', href: LEAFLET + 'leaflet.min.css'});
            leaflet = load('script', {src: LEAFLET + 'leaflet.min.js'});
        }
        return leaflet;
    }

    // where a photo was taken, on a map
    function locator(root) {
        var el = root.querySelector('.photo-locator');
        if (!el) { return; }
        loadLeaflet().then(function() {
            L.Icon.Default.imagePath = LEAFLET + 'images/';
            var at = [parseFloat(el.dataset.lat), parseFloat(el.dataset.lng)];
            // a page that scrolls shouldn't have its wheel taken by the map
            var map = L.map(el, {scrollWheelZoom: false}).setView(at, 13);
            L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
                maxZoom: 19,
                attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
            }).addTo(map);
            L.marker(at).addTo(map);
            new ResizeObserver(function() { map.invalidateSize(); }).observe(el);
        }).catch(function() {});
    }

    function build() {
        overlay = document.createElement('div');
        overlay.className = 'photo-overlay';
        overlay.hidden = true;
        overlay.innerHTML = '<div class="photo-overlay-panel"><button type="button" class="photo-overlay-close" aria-label="close">&times;</button><div class="photo-overlay-body"></div></div>';
        panel = overlay.querySelector('.photo-overlay-body');
        overlay.addEventListener('click', function(e) {
            if (e.target === overlay || e.target.closest('.photo-overlay-close')) { close(); }
        });
        document.body.appendChild(overlay);
    }

    function show(id) {
        if (!overlay) { build(); }
        current = id;
        overlay.hidden = false;
        document.body.style.overflow = 'hidden';
        overlay.scrollTop = 0;
        panel.textContent = '';
        fetch('/gallery/' + encodeURIComponent(id) + '?partial=1').then(function(r) {
            if (!r.ok) { throw new Error(r.status); }
            return r.text();
        }).then(function(html) {
            if (current !== id) { return; }
            panel.innerHTML = html;
            locator(panel);
        }).catch(function() {
            if (current !== id) { return; }
            hide();
            try { history.replaceState(null, '', location.pathname + location.search); } catch (e) {}
        });
    }

    function hide() {
        current = null;
        if (overlay) { overlay.hidden = true; panel.textContent = ''; }
        document.body.style.overflow = '';
    }

    function open(id) {
        history.pushState({photo: id}, '', '#' + id);
        opened++;
        show(id);
    }

    // back to the page as it was: the entry that opened the overlay is the one before it
    function close() {
        if (opened > 0) { history.back(); return; }
        history.replaceState(null, '', location.pathname + location.search);
        hide();
    }

    function sync() {
        var id = decodeURIComponent(location.hash.slice(1));
        if (id && id !== 'map') { if (id !== current) { show(id); } }
        else { hide(); }
    }

    document.addEventListener('click', function(e) {
        var a = e.target.closest && e.target.closest('a[data-photo]');
        if (!a || e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) { return; }
        e.preventDefault();
        open(a.dataset.photo);
    });
    document.addEventListener('keydown', function(e) {
        if (e.key === 'Escape' && current) { close(); }
    });
    window.addEventListener('popstate', function() { if (opened > 0 && !location.hash) { opened--; } sync(); });
    window.addEventListener('hashchange', sync);

    locator(document);
    sync();
})();
