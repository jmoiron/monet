/**
 * Date range filter
 * Turns a .date-filter into a calendar that drops down from a button: click a
 * first day, move the mouse to see the range, click a last day. The days go
 * in the hidden inputs named "from" and "to" (as YYYY-MM-DD), and picking days
 * clears the select named "year", which is another way to say the same thing.
 */
$(() => {
    'use strict';

    const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July',
        'August', 'September', 'October', 'November', 'December'];
    const DAYS = ['S', 'M', 'T', 'W', 'T', 'F', 'S'];

    const pad = n => String(n).padStart(2, '0');
    const iso = (y, m, d) => y + '-' + pad(m + 1) + '-' + pad(d);
    const parse = s => {
        const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s || '');
        return m ? {y: +m[1], m: +m[2] - 1, d: +m[3]} : null;
    };
    const pretty = s => {
        const p = parse(s);
        return p ? MONTHS[p.m].slice(0, 3) + ' ' + p.d + ', ' + p.y : '';
    };
    const daysIn = (y, m) => new Date(Date.UTC(y, m + 1, 0)).getUTCDate();
    const weekdayOf = (y, m) => new Date(Date.UTC(y, m, 1)).getUTCDay();

    $.fn.dateRange = function() {
        return this.each(function() {
            const box = this;
            const $box = $(box);
            const from = box.querySelector('input[name=from]');
            const to = box.querySelector('input[name=to]');
            const year = box.querySelector('select[name=year]');
            const toggle = box.querySelector('.date-toggle');
            const label = box.querySelector('.date-label');
            const pop = box.querySelector('.date-popover');

            let view = {y: 0, m: 0}; // the month on the left
            let pending = '';        // the first day, while the last is being chosen
            let hover = '';

            const setDays = (lo, hi) => {
                from.value = lo;
                to.value = hi;
                if (lo || hi) year.value = '';
                showLabel();
            };

            const showLabel = () => {
                const lo = from.value, hi = to.value;
                let text = '';
                if (lo && hi && lo !== hi) text = pretty(lo) + ' – ' + pretty(hi);
                else if (lo || hi) text = pretty(lo || hi);
                label.textContent = text;
                $box.toggleClass('has-dates', text !== '');
            };

            // the days to show as chosen: those being picked, else those set
            const range = () => {
                if (pending) {
                    const other = hover || pending;
                    return pending < other ? [pending, other] : [other, pending];
                }
                const lo = from.value || to.value, hi = to.value || from.value;
                return [lo, hi];
            };

            const shift = n => {
                const total = view.y * 12 + view.m + n;
                view = {y: Math.floor(total / 12), m: ((total % 12) + 12) % 12};
                render();
            };

            const month = (y, m) => {
                let html = '<div class="dr-month"><div class="dr-title">' + MONTHS[m] + ' ' + y + '</div><div class="dr-grid">';
                DAYS.forEach(d => { html += '<span class="dr-dow">' + d + '</span>'; });
                for (let i = weekdayOf(y, m); i > 0; i--) html += '<span></span>';
                for (let d = 1; d <= daysIn(y, m); d++) {
                    const date = iso(y, m, d);
                    html += '<button type="button" class="dr-day" data-date="' + date + '">' + d + '</button>';
                }
                return html + '</div></div>';
            };

            // classes only, so that a day isn't replaced under the mouse
            const paint = () => {
                const [lo, hi] = range();
                pop.querySelectorAll('.dr-day').forEach(b => {
                    const d = b.dataset.date;
                    const edge = d === lo || d === hi;
                    b.classList.toggle('dr-edge', !!lo && edge);
                    b.classList.toggle('dr-in', !!lo && d > lo && d < hi);
                });
            };

            const render = () => {
                const next = {y: view.y + (view.m === 11 ? 1 : 0), m: (view.m + 1) % 12};
                pop.innerHTML =
                    '<div class="dr-head">' +
                    '<button type="button" data-nav="-12" title="Back a year">«</button>' +
                    '<button type="button" data-nav="-1" title="Back a month">‹</button>' +
                    '<span class="dr-hint"></span>' +
                    '<button type="button" data-nav="1" title="Forward a month">›</button>' +
                    '<button type="button" data-nav="12" title="Forward a year">»</button>' +
                    '</div>' +
                    '<div class="dr-months">' + month(view.y, view.m) + month(next.y, next.m) + '</div>' +
                    '<div class="dr-foot"><button type="button" data-act="clear">Clear</button></div>';
                paint();
            };

            const open = () => {
                const start = parse(from.value || to.value) ||
                    (year.value ? {y: +year.value, m: 0} : null);
                const now = new Date();
                view = start ? {y: start.y, m: start.m} : {y: now.getFullYear(), m: now.getMonth()};
                pending = hover = '';
                render();
                pop.hidden = false;
            };
            const close = () => { pop.hidden = true; pending = hover = ''; };

            toggle.addEventListener('click', e => {
                e.preventDefault();
                if (pop.hidden) open(); else close();
            });

            pop.addEventListener('click', e => {
                const b = e.target.closest('button');
                if (!b) return;
                e.preventDefault();
                if (b.dataset.nav) {
                    shift(+b.dataset.nav);
                } else if (b.dataset.act === 'clear') {
                    pending = hover = '';
                    setDays('', '');
                    paint();
                } else if (b.dataset.date) {
                    const d = b.dataset.date;
                    if (!pending) {
                        pending = d;
                        hover = '';
                        paint();
                    } else {
                        const [lo, hi] = pending < d ? [pending, d] : [d, pending];
                        setDays(lo, hi);
                        close();
                    }
                }
            });

            pop.addEventListener('mouseover', e => {
                const d = e.target.dataset && e.target.dataset.date;
                if (pending && d && d !== hover) {
                    hover = d;
                    paint();
                }
            });

            // a year and a span of days are two ways to say the same thing
            year.addEventListener('change', () => {
                if (year.value) setDays('', '');
            });

            document.addEventListener('click', e => {
                // (the path, as a button may have been replaced by the click itself)
                if (!pop.hidden && !e.composedPath().includes(box)) close();
            });
            document.addEventListener('keydown', e => {
                if (e.key === 'Escape' && !pop.hidden) close();
            });

            showLabel();
        });
    };

    $('.date-filter').dateRange();
});
