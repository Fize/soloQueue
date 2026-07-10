(function(){
  var ALLOWED_PROPS = {
    color:1, 'background-color':1, 'font-size':1, 'font-weight':1, 'line-height':1,
    'padding-top':1, 'padding-right':1, 'padding-bottom':1, 'padding-left':1,
    'border-radius':1, 'text-align':1, 'font-family':1
  };

  var commentEnabled = false;
  var mode = 'picker'; // 'picker' | 'pod' | 'interact'
  var hoveredId = null;
  var drawing = false;
  var stroke = [];
  var selectedEl = null;
  var hoveredEl = null;

  function esc(value){ try { return window.CSS && CSS.escape ? CSS.escape(value) : String(value).replace(/"/g, '\\"'); } catch (_) { return String(value); } }

  function styleSnapshot(el){
    try {
      var cs = window.getComputedStyle(el);
      return {
        color: cs.color,
        backgroundColor: cs.backgroundColor,
        fontSize: cs.fontSize,
        fontWeight: cs.fontWeight,
        lineHeight: cs.lineHeight,
        paddingTop: cs.paddingTop,
        paddingRight: cs.paddingRight,
        paddingBottom: cs.paddingBottom,
        paddingLeft: cs.paddingLeft,
        borderRadius: cs.borderTopLeftRadius,
        textAlign: cs.textAlign,
        fontFamily: cs.fontFamily
      };
    } catch (_) { return null; }
  }

  function visibleTarget(el){
    if (!el || !el.getBoundingClientRect) return false;
    if (el === document.documentElement || el === document.body) return false;
    if (/^(script|style|template|meta|link|title|noscript)$/.test(el.tagName ? el.tagName.toLowerCase() : '')) return false;
    try {
      var rect = el.getBoundingClientRect();
      if (rect.width < 1 || rect.height < 1) return false;
      var cs = window.getComputedStyle(el);
      if (cs.display === 'none' || cs.visibility === 'hidden' || cs.pointerEvents === 'none') return false;
    } catch (_) {
      return false;
    }
    return true;
  }

  function meaningfulDomFallbackTarget(el) {
    if (!visibleTarget(el)) return false;
    var tag = el.tagName ? el.tagName.toLowerCase() : '';
    if (/^(a|button|input|textarea|select|label|img|video|canvas|h1|h2|h3|h4|h5|h6|p|li|td|th|section|article|main|aside|nav|div)$/.test(tag)) {
      return true;
    }
    return true;
  }

  function domSelectorFor(el){
    if (!el || !el.tagName || el === document.documentElement || el === document.body) return null;
    var parts = [];
    var node = el;
    while (node && node !== document.documentElement && node !== document.body) {
      var tag = node.tagName ? node.tagName.toLowerCase() : '';
      if (!tag || /^(script|style|template|meta|link|title|noscript)$/.test(tag)) return null;
      var parent = node.parentElement;
      if (!parent) return null;
      var index = 1;
      var sibling = node.previousElementSibling;
      while (sibling) {
        if (sibling.tagName && sibling.tagName.toLowerCase() === tag) index++;
        sibling = sibling.previousElementSibling;
      }
      parts.unshift(tag + ':nth-of-type(' + index + ')');
      node = parent;
    }
    if (!parts.length) return null;
    return 'body > ' + parts.join(' > ');
  }

  function targetFrom(el, clickedEl){
    var id = el.getAttribute('data-od-id');
    var selector = id ? '[data-od-id="' + esc(id) + '"]' : null;
    if (!id && meaningfulDomFallbackTarget(el)) {
      selector = domSelectorFor(el);
      if (selector) id = 'dom:' + selector;
    }
    if (!id || !selector) return null;

    var rect = el.getBoundingClientRect();
    var tag = el.tagName ? el.tagName.toLowerCase() : 'element';
    var cls = typeof el.className === 'string' && el.className.trim() ? '.' + el.className.trim().split(/\s+/).slice(0,2).join('.') : '';
    var html = '';
    try { html = (el.outerHTML || '').replace(/\s+/g, ' ').match(/^<[^>]+>/)?.[0] || ''; } catch (_) {}

    return {
      type: 'od:comment-target',
      elementId: id,
      selector: selector,
      label: tag + cls,
      text: (el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 160),
      position: { x: Math.round(rect.x), y: Math.round(rect.y), width: Math.round(rect.width), height: Math.round(rect.height) },
      htmlHint: html.slice(0, 180),
      style: styleSnapshot(el)
    };
  }

  function closestTarget(event){
    var clicked = event.target;
    var el = clicked;
    var fallback = null;
    while (el && el !== document.documentElement) {
      if (el.getAttribute && el.hasAttribute('data-od-id')) {
        return { target: el, clicked: clicked };
      }
      if (!fallback && meaningfulDomFallbackTarget(el)) fallback = el;
      el = el.parentElement;
    }
    return fallback ? { target: fallback, clicked: clicked } : null;
  }

  window.addEventListener('message', function(ev){
    var data = ev && ev.data;
    if (!data || !data.type) return;

    if (data.type === 'od:comment-mode') {
      commentEnabled = !!data.enabled;
      mode = data.mode === 'pod' ? 'pod' : data.mode === 'interact' ? 'interact' : 'picker';
      document.documentElement.toggleAttribute('data-od-comment-mode', commentEnabled && mode !== 'interact');
      document.documentElement.setAttribute('data-od-comment-mode-kind', mode);

      if (!commentEnabled || (mode !== 'pod' && mode !== 'interact')) {
        drawing = false;
        stroke = [];
        try { window.parent.postMessage({ type: 'od:pod-clear' }, '*'); } catch (_) {}
      }
      return;
    }
  });

  document.addEventListener('mouseover', function(ev){
    if (!commentEnabled || mode !== 'picker') return;
    var result = closestTarget(ev);
    if (!result) return;
    var payload = targetFrom(result.target, result.clicked);
    if (!payload || payload.elementId === hoveredId) return;
    hoveredId = payload.elementId;
    hoveredEl = result.target;
    window.parent.postMessage(Object.assign({}, payload, { type: 'od:comment-hover' }), '*');
  }, true);

  document.addEventListener('mouseout', function(ev){
    if (!commentEnabled || mode !== 'picker') return;
    var result = closestTarget(ev);
    if (!result) return;
    var next = ev.relatedTarget;
    while (next && next !== document.documentElement) {
      if (next === result.target) return;
      next = next.parentElement;
    }
    hoveredId = null;
    hoveredEl = null;
    window.parent.postMessage({ type: 'od:comment-leave' }, '*');
  }, true);

  document.addEventListener('click', function(ev){
    if (!commentEnabled) return;

    if (mode === 'picker') {
      var result = closestTarget(ev);
      if (result) {
        ev.preventDefault();
        ev.stopPropagation();
        var payload = targetFrom(result.target, result.clicked);
        if (payload) {
          selectedEl = result.target;
          window.parent.postMessage(payload, '*');
        }
      }
      return;
    }

    if (mode === 'interact') {
      var a = ev.target.closest && ev.target.closest('a[href]');
      if (a) {
        var href = a.getAttribute('href') || '';
        var isRealAnchor = href.length > 1 && href.charAt(0) === '#';
        var isJavascript = href.indexOf('javascript:') === 0;
        if (!isRealAnchor && !isJavascript) {
          ev.preventDefault();
        }
        return;
      }
      var form = ev.target.closest && ev.target.closest('form');
      if (form) {
        ev.preventDefault();
        return;
      }
      return;
    }
  }, true);

  window.addEventListener('scroll', function() {
    window.parent.postMessage({
      type: 'od:iframe-scroll',
      x: window.scrollX || window.pageXOffset,
      y: window.scrollY || window.pageYOffset
    }, '*');

    if (commentEnabled && mode === 'picker') {
      if (selectedEl) {
        var payload = targetFrom(selectedEl, selectedEl);
        if (payload) {
          window.parent.postMessage(Object.assign({}, payload, { type: 'od:comment-scroll-selected' }), '*');
        }
      }
      if (hoveredEl) {
        var payload = targetFrom(hoveredEl, hoveredEl);
        if (payload) {
          window.parent.postMessage(Object.assign({}, payload, { type: 'od:comment-scroll-hovered' }), '*');
        }
      }
    }
  }, true);

  // Pod drawing
  function relativePoint(ev){
    return { x: Math.round(ev.clientX), y: Math.round(ev.clientY) };
  }

  function postStroke(type){
    window.parent.postMessage({ type: type, points: stroke.slice() }, '*');
  }

  document.addEventListener('pointerdown', function(ev){
    if (!commentEnabled || mode !== 'pod' || ev.button !== 0) return;
    drawing = true;
    stroke = [relativePoint(ev)];
    ev.preventDefault();
    ev.stopPropagation();
    postStroke('od:pod-stroke');
  }, true);

  document.addEventListener('pointermove', function(ev){
    if (!drawing || mode !== 'pod') return;
    var point = relativePoint(ev);
    var last = stroke[stroke.length - 1];
    if (last && Math.hypot(last.x - point.x, last.y - point.y) < 4) return;
    stroke.push(point);
    ev.preventDefault();
    ev.stopPropagation();
    postStroke('od:pod-stroke');
  }, true);

  function finishStroke(ev){
    if (!drawing || mode !== 'pod') return;
    drawing = false;
    if (ev) {
      ev.preventDefault();
      ev.stopPropagation();
    }
    postStroke('od:pod-select');
  }

  document.addEventListener('pointerup', finishStroke, true);
  document.addEventListener('pointercancel', finishStroke, true);

})();
