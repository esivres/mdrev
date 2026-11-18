/*
 * mdrev preview — renders the document and overlays the review threads on it.
 *
 * Build-free: every dependency is a global script loaded by index.html. The
 * server streams the document and the review over /events; this file renders
 * content, anchors cards to source lines, and posts comments, replies and
 * resolves back through the JSON API.
 */
(function () {
  'use strict';

  var AUTHOR_KEY = 'mdrev.author';
  var TYPE_LABELS = { '': 'Comment', issue: 'Issue', question: 'Question', suggestion: 'Suggestion' };
  var BADGE_LABELS = { '': 'comment', issue: 'issue', question: 'question', suggestion: 'suggestion' };

  var state = {
    text: '',
    threads: [],
    showResolved: false
  };

  // ---- DOM ---------------------------------------------------------------

  var docEl = document.getElementById('doc');
  var lostSection = document.getElementById('lost');
  var lostThreadsEl = document.getElementById('lost-threads');
  var lostCountEl = document.getElementById('lost-count');
  var fileNameEl = document.getElementById('file-name');
  var threadCountEl = document.getElementById('thread-count');
  var showResolvedEl = document.getElementById('show-resolved');
  var connEl = document.getElementById('conn');
  var connLabelEl = document.getElementById('conn-label');
  var toastsEl = document.getElementById('toasts');

  var addCommentBtn = document.getElementById('add-comment');
  var notesEl = document.getElementById('notes');
  var compose = document.getElementById('compose');
  var composeLineEl = document.getElementById('compose-line');
  var composeTypesEl = document.getElementById('compose-types');
  var composeQuoteEl = document.getElementById('compose-quote');
  var composeTextEl = document.getElementById('compose-text');
  var composeAuthorEl = document.getElementById('compose-author');
  var composeCancelBtn = document.getElementById('compose-cancel');
  var composeSubmitBtn = document.getElementById('compose-submit');

  // ---- markdown ----------------------------------------------------------

  // Blocks that carry an anchor line. The trick is markdown-preview.nvim's
  // linenumbers.js: a token's map[0] is the 0-based source line it starts on.
  var ANCHOR_TOKENS = {
    paragraph_open: true,
    heading_open: true,
    list_item_open: true,
    table_open: true,
    fence: true,
    code_block: true,
    hr: true
  };

  var md = window.markdownit({
    html: false,
    xhtmlOut: false,
    breaks: false,
    linkify: true,
    typographer: true,
    highlight: function (str, lang) {
      if (!lang || !window.hljs || !window.hljs.getLanguage(lang)) {
        return ''; // let markdown-it escape the block
      }
      try {
        // Return the inner markup only: markdown-it then wraps it and keeps
        // the token attributes, including data-source-line.
        return window.hljs.highlight(str, { language: lang, ignoreIllegals: true }).value;
      } catch (err) {
        return '';
      }
    }
  });

  if (window.markdownitTaskLists) {
    md.use(window.markdownitTaskLists);
  }

  md.core.ruler.push('mdrev_source_lines', function (coreState) {
    if (coreState.env && coreState.env.noSourceLines) {
      return;
    }
    for (var i = 0; i < coreState.tokens.length; i++) {
      var token = coreState.tokens[i];
      if (ANCHOR_TOKENS[token.type] && token.map) {
        token.attrSet('data-source-line', String(token.map[0]));
      }
    }
  });

  // Comment text is markdown too, but its own blocks must not look like
  // document blocks to the anchoring code.
  function renderCommentMarkdown(text) {
    return md.render(text || '', { noSourceLines: true });
  }

  // markdown-it's fence renderer puts token attributes on the inner code
  // element; the anchor belongs to the block the reader points at.
  function normalizeAnchors(root) {
    Array.prototype.forEach.call(root.querySelectorAll('pre > code[data-source-line]'), function (code) {
      var pre = code.parentElement;
      pre.setAttribute('data-source-line', code.getAttribute('data-source-line'));
      code.removeAttribute('data-source-line');
    });
  }

  function renderMath(root) {
    if (!window.renderMathInElement) {
      return;
    }
    try {
      window.renderMathInElement(root, {
        delimiters: [
          { left: '$$', right: '$$', display: true },
          { left: '$', right: '$', display: false }
        ],
        throwOnError: false
      });
    } catch (err) {
      // A malformed expression must not take the page down.
    }
  }

  var isDark = function () {
    return !!(window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches);
  };

  var mermaidReady = false;
  var renderSeq = 0;

  function renderContent() {
    var seq = ++renderSeq;
    // The blocks the hover button points at are about to disappear.
    hoverBlock = null;
    hideAddButton();
    docEl.innerHTML = md.render(state.text || '');
    normalizeAnchors(docEl);
    renderMath(docEl);
    renderOverlay();
    renderDiagrams(seq);
  }

  // Fenced mermaid blocks become diagrams; a parse failure restores the code
  // block with a caption instead of leaving mermaid's error graphic in place.
  function renderDiagrams(seq) {
    if (!window.mermaid) {
      return;
    }
    var codes = docEl.querySelectorAll('pre > code.language-mermaid, pre > code.lang-mermaid');
    if (!codes.length) {
      return;
    }
    if (!mermaidReady) {
      window.mermaid.initialize({ startOnLoad: false, theme: isDark() ? 'dark' : 'default' });
      mermaidReady = true;
    }
    // Mermaid's renderer is not reentrant; queue the blocks so diagrams that
    // arrive in one document do not race for its internal state.
    var queue = Promise.resolve();
    Array.prototype.forEach.call(codes, function (code) {
      if (seq !== renderSeq) {
        return;
      }
      var pre = code.parentNode;
      var sourceLine = pre.getAttribute('data-source-line');
      var source = code.textContent;
      var holder = document.createElement('div');
      holder.className = 'mermaid';
      holder.textContent = source;
      if (sourceLine !== null) {
        holder.setAttribute('data-source-line', sourceLine);
      }
      pre.parentNode.replaceChild(holder, pre);
      queue = queue.then(function () {
        if (seq !== renderSeq) {
          return;
        }
        return Promise.resolve().then(function () {
          return window.mermaid.run({ nodes: [holder] });
        }).catch(function () {
          if (seq !== renderSeq) {
            return;
          }
          var fallback = document.createElement('pre');
          fallback.className = 'mermaid-failed';
          if (sourceLine !== null) {
            fallback.setAttribute('data-source-line', sourceLine);
          }
          var fallbackCode = document.createElement('code');
          fallbackCode.textContent = source;
          fallback.appendChild(fallbackCode);
          var caption = document.createElement('p');
          caption.className = 'mermaid-error';
          caption.textContent = 'Diagram could not be rendered.';
          var parent = holder.parentNode;
          if (parent) {
            parent.replaceChild(caption, holder);
            parent.insertBefore(fallback, caption);
          }
        });
      });
    });
    // The overlay was built against the code blocks before they became
    // diagrams; rebuild it once the DOM has settled.
    queue.then(function () {
      if (seq === renderSeq) {
        renderOverlay();
      }
    });
  }

  // ---- helpers -----------------------------------------------------------

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) {
      node.className = className;
    }
    if (text !== undefined && text !== null) {
      node.textContent = text;
    }
    return node;
  }

  function button(label, className, onClick) {
    var node = el('button', className, label);
    node.type = 'button';
    if (onClick) {
      node.addEventListener('click', onClick);
    }
    return node;
  }

  function toast(message, kind) {
    var node = el('div', kind === 'error' ? 'toast error' : 'toast', message);
    toastsEl.appendChild(node);
    var ttl = kind === 'error' ? 4500 : 2600;
    window.setTimeout(function () {
      node.classList.add('leaving');
      window.setTimeout(function () {
        if (node.parentNode) {
          node.parentNode.removeChild(node);
        }
      }, 250);
    }, ttl);
  }

  function postJSON(url, body) {
    return fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }).then(function (resp) {
      return resp.json().catch(function () { return null; }).then(function (data) {
        if (!resp.ok || !data || data.ok === false) {
          var message = data && data.error ? data.error : 'request failed (' + resp.status + ')';
          throw new Error(message);
        }
        return data;
      });
    });
  }

  function formatTimestamp(value) {
    if (!value) {
      return '';
    }
    var date = new Date(value);
    if (isNaN(date.getTime())) {
      return value;
    }
    return date.toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
  }

  // ---- thread cards ------------------------------------------------------

  // Reply drafts survive the overlay rebuild that every sidecar change
  // causes. Keyed by thread id; a closed form keeps its text for the next open.
  var replyDrafts = {};

  function saveReplyDraft(thread, form) {
    var textarea = form.querySelector('textarea');
    replyDrafts[thread.id] = {
      open: !form.hidden,
      text: textarea ? textarea.value : ''
    };
  }

  function threadCard(thread) {
    var card = el('article', 'thread-card');
    card.setAttribute('data-id', thread.id || '');
    if (thread.resolved) {
      card.classList.add('resolved');
    }

    var head = el('div', 'thread-head');
    var type = thread.type || '';
    head.appendChild(el('span', 'badge badge-' + (type || 'comment'), BADGE_LABELS[type] || type));
    if (thread.resolved && thread.x_outcome) {
      head.appendChild(el('span', 'badge badge-outcome', thread.x_outcome));
    }
    if (thread.author) {
      head.appendChild(el('span', 'thread-author', thread.author));
    }
    var stamp = formatTimestamp(thread.timestamp);
    if (stamp) {
      var time = el('time', 'thread-time', stamp);
      if (thread.timestamp) {
        time.dateTime = thread.timestamp;
      }
      head.appendChild(time);
    }
    card.appendChild(head);

    var body = el('div', 'thread-text');
    body.innerHTML = renderCommentMarkdown(thread.text);
    card.appendChild(body);
    renderMath(body);

    if (thread.selected_text) {
      card.appendChild(el('pre', 'thread-quote', thread.selected_text));
    }

    if (thread.x_suggested_text) {
      card.appendChild(suggestionDiff(thread.selected_text || '', thread.x_suggested_text));
    }

    if (thread.replies && thread.replies.length) {
      var replies = el('div', 'thread-replies');
      thread.replies.forEach(function (reply) {
        replies.appendChild(replyItem(reply));
      });
      card.appendChild(replies);
    }

    if (!thread.resolved) {
      var actions = el('div', 'thread-actions');
      var form = replyForm(thread);
      var replyBtn = button('Reply', 'btn small', function () {
        form.hidden = !form.hidden;
        saveReplyDraft(thread, form);
        if (!form.hidden) {
          form.querySelector('textarea').focus();
        }
      });
      actions.appendChild(replyBtn);
      actions.appendChild(button('Resolve', 'btn small', function () {
        resolveThread(thread);
      }));
      card.appendChild(actions);
      card.appendChild(form);
    }

    return card;
  }

  function replyItem(reply) {
    var item = el('div', 'reply');
    var head = el('div', 'reply-head');
    if (reply.author) {
      head.appendChild(el('span', 'reply-author', reply.author));
    }
    var stamp = formatTimestamp(reply.timestamp);
    if (stamp) {
      head.appendChild(el('time', 'reply-time', stamp));
    }
    if (head.childNodes.length) {
      item.appendChild(head);
    }
    var body = el('div', 'reply-text');
    body.innerHTML = renderCommentMarkdown(reply.text);
    item.appendChild(body);
    renderMath(body);
    return item;
  }

  function replyForm(thread) {
    var form = el('div', 'reply-form');
    var draft = replyDrafts[thread.id];
    form.hidden = draft ? !draft.open : true;
    var textarea = document.createElement('textarea');
    textarea.rows = 2;
    textarea.placeholder = 'Write a reply…';
    if (draft && draft.text) {
      textarea.value = draft.text;
    }
    form.appendChild(textarea);
    textarea.addEventListener('input', function () {
      saveReplyDraft(thread, form);
    });

    var row = el('div', 'reply-actions');
    row.appendChild(el('span', 'compose-hint', 'Ctrl/Cmd+Enter'));
    var sending = false;

    function send() {
      if (sending) {
        return;
      }
      var text = textarea.value.trim();
      if (!text) {
        textarea.focus();
        return;
      }
      sending = true;
      sendBtn.disabled = true;
      postJSON('/api/reply', { id: thread.id, text: text }).then(function (data) {
        delete replyDrafts[thread.id];
        if (data.comment) {
          if (!thread.replies) {
            thread.replies = [];
          }
          thread.replies.push(data.comment);
          renderOverlay();
        }
        toast('Reply added');
      }).catch(function (err) {
        toast(err.message || 'Could not add the reply', 'error');
      }).then(function () {
        sending = false;
        sendBtn.disabled = false;
      });
    }

    var sendBtn = button('Send', 'btn small primary', send);
    textarea.addEventListener('keydown', function (event) {
      if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') {
        event.preventDefault();
        send();
      }
    });
    row.appendChild(sendBtn);
    form.appendChild(row);
    return form;
  }

  // A readable word diff without a library: an LCS over whitespace tokens.
  // Oversized quotes fall back to before/after blocks.
  function diffTokens(oldText, newText) {
    var a = String(oldText).match(/\S+\s*|\s+/g) || [];
    var b = String(newText).match(/\S+\s*|\s+/g) || [];
    if (a.length * b.length > 200000) {
      return null;
    }
    var n = a.length;
    var m = b.length;
    var dp = [];
    var i;
    var j;
    for (i = 0; i <= n; i++) {
      dp.push(new Uint32Array(m + 1));
    }
    for (i = n - 1; i >= 0; i--) {
      for (j = m - 1; j >= 0; j--) {
        dp[i][j] = a[i].trim() === b[j].trim()
          ? dp[i + 1][j + 1] + 1
          : Math.max(dp[i + 1][j], dp[i][j + 1]);
      }
    }
    var ops = [];
    function add(kind, text) {
      var last = ops[ops.length - 1];
      if (last && last.kind === kind) {
        last.text += text;
      } else {
        ops.push({ kind: kind, text: text });
      }
    }
    i = 0;
    j = 0;
    while (i < n && j < m) {
      if (a[i].trim() === b[j].trim()) {
        add('same', a[i]);
        i++;
        j++;
      } else if (dp[i + 1][j] >= dp[i][j + 1]) {
        add('del', a[i]);
        i++;
      } else {
        add('ins', b[j]);
        j++;
      }
    }
    while (i < n) {
      add('del', a[i]);
      i++;
    }
    while (j < m) {
      add('ins', b[j]);
      j++;
    }
    return ops;
  }

  function suggestionDiff(oldText, newText) {
    var wrapper = el('div', 'suggestion');
    wrapper.appendChild(el('div', 'suggestion-label', 'Suggested change'));
    var ops = diffTokens(oldText, newText);
    if (!ops) {
      var blocks = el('div', 'diff-blocks');
      var before = el('pre', 'diff-before', oldText);
      var after = el('pre', 'diff-after', newText);
      blocks.appendChild(before);
      blocks.appendChild(after);
      wrapper.appendChild(blocks);
      return wrapper;
    }
    var diff = el('div', 'diff');
    ops.forEach(function (op) {
      if (op.kind === 'same') {
        diff.appendChild(document.createTextNode(op.text));
      } else {
        var node = document.createElement(op.kind === 'del' ? 'del' : 'ins');
        node.textContent = op.text;
        diff.appendChild(node);
      }
    });
    wrapper.appendChild(diff);
    return wrapper;
  }

  // ---- overlay -----------------------------------------------------------

  function allBlocks() {
    return Array.prototype.slice.call(docEl.querySelectorAll('[data-source-line]'));
  }

  // The nearest block at or above the comment's line. A comment on line N
  // belongs to the last block that starts before it: the token map is 0-based,
  // so a block starting on line N has map[N - 1]. A line past the end of the
  // document belongs to no block — the anchor is gone.
  function anchorFor(thread, blocks) {
    var line = Number(thread.line);
    if (thread.orphaned || !isFinite(line) || line < 1) {
      return null;
    }
    var lineCount = state.text ? state.text.split('\n').length : 0;
    if (line > lineCount) {
      return null;
    }
    var lastLine = line - 1;
    var best = null;
    var bestLine = -Infinity;
    for (var i = 0; i < blocks.length; i++) {
      var blockLine = Number(blocks[i].getAttribute('data-source-line'));
      if (!isFinite(blockLine) || blockLine > lastLine) {
        continue;
      }
      if (blockLine > bestLine) {
        best = blocks[i];
        bestLine = blockLine;
      }
    }
    return best;
  }

  // Cards follow the outermost element the block belongs to, so a comment on
  // a list item lands below the whole list rather than inside it.
  function topLevel(node) {
    var current = node;
    while (current.parentNode && current.parentNode !== docEl) {
      current = current.parentNode;
    }
    return current;
  }

  function updateToolbar() {
    var open = 0;
    state.threads.forEach(function (thread) {
      if (!thread.resolved) {
        open++;
      }
    });
    threadCountEl.textContent = open + ' open';
  }

  // ---- margin notes ------------------------------------------------------

  // Wide screens put cards in a right-hand gutter aligned with their anchor
  // blocks; narrow ones fall back to cards below the block. The media query
  // in preview.css and this matchMedia must agree on the breakpoint.
  var wideQuery = window.matchMedia('(min-width: 1100px)');
  var notesEntries = [];
  var layoutTimer = 0;
  var cardObserver = null;

  function applyNotesLayout() {
    if (!wideQuery.matches) {
      return;
    }
    var notesTop = notesEl.getBoundingClientRect().top;
    var cursor = 0;
    notesEntries.forEach(function (entry) {
      if (!entry.card.isConnected) {
        return;
      }
      var blockTop = entry.block.getBoundingClientRect().top - notesTop;
      // Cards on short blocks cascade instead of overlapping.
      var top = Math.max(blockTop, cursor);
      entry.card.style.top = top + 'px';
      cursor = top + entry.card.offsetHeight + 12;
    });
  }

  // A mermaid render, a late image or a font swap changes heights after the
  // first layout; coalesce those into one pass.
  function scheduleNotesLayout() {
    window.clearTimeout(layoutTimer);
    layoutTimer = window.setTimeout(applyNotesLayout, 100);
  }

  function stackFor(root, stacks) {
    var stack = stacks.get(root);
    if (!stack) {
      stack = el('div', 'thread-stack');
      if (root.nextSibling) {
        root.parentNode.insertBefore(stack, root.nextSibling);
      } else {
        root.parentNode.appendChild(stack);
      }
      stacks.set(root, stack);
    }
    return stack;
  }

  function renderOverlay() {
    Array.prototype.forEach.call(docEl.querySelectorAll('.thread-stack'), function (stack) {
      stack.parentNode.removeChild(stack);
    });
    Array.prototype.forEach.call(docEl.querySelectorAll('.has-threads'), function (block) {
      block.classList.remove('has-threads');
    });
    lostThreadsEl.textContent = '';
    notesEl.textContent = '';
    notesEntries = [];
    if (cardObserver) {
      cardObserver.disconnect();
    }

    var blocks = allBlocks();
    var blockOrder = new Map();
    blocks.forEach(function (block, index) {
      blockOrder.set(block, index);
    });
    var wide = wideQuery.matches;
    var stacks = new Map();
    var notes = [];
    var lost = 0;
    var threads = state.threads.slice().sort(function (a, b) {
      var byLine = (Number(a.line) || 0) - (Number(b.line) || 0);
      if (byLine !== 0) {
        return byLine;
      }
      return String(a.timestamp || '').localeCompare(String(b.timestamp || ''));
    });

    threads.forEach(function (thread) {
      if (thread.resolved && !state.showResolved) {
        return;
      }
      var block = anchorFor(thread, blocks);
      var card = threadCard(thread);
      if (!block) {
        lostThreadsEl.appendChild(card);
        lost++;
        return;
      }
      block.classList.add('has-threads');
      if (wide) {
        notesEl.appendChild(card);
        notes.push({ card: card, block: block, order: blockOrder.get(block) });
        if (cardObserver) {
          cardObserver.observe(card);
        }
      } else {
        stackFor(topLevel(block), stacks).appendChild(card);
      }
    });

    notes.sort(function (a, b) {
      return a.order - b.order;
    });
    notesEntries = notes;
    applyNotesLayout();

    lostSection.hidden = lost === 0;
    lostCountEl.textContent = lost ? '(' + lost + ')' : '';
    updateToolbar();
  }

  function upsertThread(thread) {
    var seen = false;
    for (var i = 0; i < state.threads.length; i++) {
      if (state.threads[i].id === thread.id) {
        seen = true;
        break;
      }
    }
    if (!seen) {
      state.threads.push(thread);
    }
    renderOverlay();
  }

  function resolveThread(thread) {
    if (thread.resolved) {
      return;
    }
    var previousOutcome = thread.x_outcome;
    delete replyDrafts[thread.id];
    thread.resolved = true;
    thread.x_outcome = 'resolved';
    renderOverlay();
    postJSON('/api/resolve', { id: thread.id }).then(function () {
      toast('Thread resolved');
    }).catch(function (err) {
      thread.resolved = false;
      thread.x_outcome = previousOutcome;
      renderOverlay();
      toast(err.message || 'Could not resolve the thread', 'error');
    });
  }

  // ---- compose -----------------------------------------------------------

  var hoverBlock = null;
  var selectionTarget = null;
  var buttonMode = null;
  var hideTimer = 0;
  var selectionTimer = 0;
  var composeLine = 1;
  var composeType = '';
  var composeQuote = '';
  var posting = false;

  function placeButton(top, left) {
    addCommentBtn.hidden = false;
    addCommentBtn.style.top = Math.max(8, top) + 'px';
    addCommentBtn.style.left = Math.max(8, left) + 'px';
  }

  function positionAddButton(block) {
    var rect = block.getBoundingClientRect();
    if (rect.bottom < 0 || rect.top > window.innerHeight) {
      addCommentBtn.hidden = true;
      return;
    }
    buttonMode = 'block';
    placeButton(rect.top + 2, rect.left - 34);
  }

  // The selection button sits above the end of the highlighted run.
  function positionSelectionButton() {
    if (!selectionTarget || !compose.hidden) {
      return;
    }
    var range = selectionTarget.range;
    var rects = range.getClientRects();
    var rect = rects.length ? rects[rects.length - 1] : range.getBoundingClientRect();
    if (!rect || (!rect.width && !rect.height)) {
      hideAddButton();
      return;
    }
    var top = rect.top - 32;
    if (top < 8) {
      top = rect.bottom + 6;
    }
    buttonMode = 'selection';
    placeButton(top, rect.right - 13);
  }

  function hideAddButton() {
    window.clearTimeout(hideTimer);
    addCommentBtn.hidden = true;
    buttonMode = null;
  }

  function armHide() {
    if (buttonMode !== 'block') {
      return; // the selection button belongs to the selection, not the pointer
    }
    window.clearTimeout(hideTimer);
    hideTimer = window.setTimeout(function () {
      if (buttonMode === 'block') {
        addCommentBtn.hidden = true;
        buttonMode = null;
      }
    }, 220);
  }

  function updateSelection() {
    if (!compose.hidden) {
      return;
    }
    var sel = window.getSelection();
    if (!sel || sel.rangeCount === 0 || sel.isCollapsed) {
      clearSelectionTarget();
      return;
    }
    var quote = sel.toString().trim();
    if (!quote) {
      clearSelectionTarget();
      return;
    }
    var range = sel.getRangeAt(0);
    var start = range.startContainer;
    var element = start.nodeType === Node.ELEMENT_NODE ? start : start.parentElement;
    var block = element && element.closest ? element.closest('[data-source-line]') : null;
    if (!block || !docEl.contains(block)) {
      clearSelectionTarget();
      return;
    }
    selectionTarget = {
      block: block,
      range: range,
      quote: quote,
      line: Number(block.getAttribute('data-source-line')) + 1
    };
    positionSelectionButton();
  }

  function clearSelectionTarget() {
    selectionTarget = null;
    if (buttonMode === 'selection') {
      buttonMode = null;
      addCommentBtn.hidden = true;
    }
  }

  function scheduleSelection() {
    window.clearTimeout(selectionTimer);
    selectionTimer = window.setTimeout(updateSelection, 60);
  }

  docEl.addEventListener('mousemove', function (event) {
    if (!compose.hidden) {
      return;
    }
    if (buttonMode === 'selection' && selectionTarget) {
      return;
    }
    var target = event.target;
    var block = target && target.closest ? target.closest('[data-source-line]') : null;
    if (!block) {
      armHide();
      return;
    }
    window.clearTimeout(hideTimer);
    hoverBlock = block;
    positionAddButton(block);
  });

  docEl.addEventListener('mouseleave', armHide);
  docEl.addEventListener('mouseup', function () {
    window.setTimeout(updateSelection, 0);
  });
  document.addEventListener('selectionchange', scheduleSelection);

  addCommentBtn.addEventListener('mouseenter', function () {
    window.clearTimeout(hideTimer);
  });
  addCommentBtn.addEventListener('mouseleave', armHide);
  addCommentBtn.addEventListener('mousedown', function (event) {
    // Keep the captured selection alive through the click that follows.
    event.preventDefault();
    window.clearTimeout(selectionTimer);
  });

  window.addEventListener('scroll', function () {
    if (!compose.hidden) {
      return;
    }
    if (buttonMode === 'selection' && selectionTarget) {
      positionSelectionButton();
      return;
    }
    hoverBlock = null;
    hideAddButton();
  }, { passive: true });

  addCommentBtn.addEventListener('click', function (event) {
    event.preventDefault();
    var anchorRect = addCommentBtn.getBoundingClientRect();
    addCommentBtn.hidden = true;
    if (buttonMode === 'selection' && selectionTarget) {
      var quote = selectionTarget.quote;
      var line = selectionTarget.line;
      buttonMode = null;
      openCompose(line, anchorRect, quote);
      return;
    }
    buttonMode = null;
    if (!hoverBlock) {
      return;
    }
    var blockLine = Number(hoverBlock.getAttribute('data-source-line'));
    if (!isFinite(blockLine)) {
      return;
    }
    openCompose(blockLine + 1, anchorRect, '');
  });

  function truncateQuote(quote) {
    var flat = quote.replace(/\s+/g, ' ').trim();
    if (flat.length <= 120) {
      return flat;
    }
    return flat.slice(0, 117).replace(/\s+$/, '') + '…';
  }

  function openCompose(line, anchorRect, quote) {
    composeLine = line;
    composeQuote = quote || '';
    composeLineEl.textContent = 'line ' + line;
    if (composeQuote) {
      composeQuoteEl.textContent = truncateQuote(composeQuote);
      composeQuoteEl.hidden = false;
    } else {
      composeQuoteEl.textContent = '';
      composeQuoteEl.hidden = true;
    }
    compose.hidden = false;
    var rect = compose.getBoundingClientRect();
    var left = Math.min(Math.max(8, anchorRect.left), window.innerWidth - rect.width - 8);
    var top = anchorRect.bottom + 6;
    if (top + rect.height > window.innerHeight - 8) {
      top = Math.max(8, anchorRect.top - rect.height - 6);
    }
    compose.style.left = left + 'px';
    compose.style.top = top + 'px';
    composeTextEl.focus();
  }

  function closeCompose(clear) {
    compose.hidden = true;
    if (clear) {
      composeTextEl.value = '';
      selectType('');
      composeQuote = '';
      composeQuoteEl.textContent = '';
      composeQuoteEl.hidden = true;
      var sel = window.getSelection();
      if (sel) {
        sel.removeAllRanges();
      }
      clearSelectionTarget();
    }
  }

  function selectType(type) {
    composeType = type;
    Array.prototype.forEach.call(composeTypesEl.querySelectorAll('button[data-type]'), function (node) {
      node.classList.toggle('active', (node.getAttribute('data-type') || '') === type);
    });
    composeSubmitBtn.textContent = TYPE_LABELS[type] || 'Comment';
  }

  composeTypesEl.addEventListener('click', function (event) {
    var node = event.target.closest ? event.target.closest('button[data-type]') : null;
    if (!node) {
      return;
    }
    selectType(node.getAttribute('data-type') || '');
  });

  composeCancelBtn.addEventListener('click', function () {
    closeCompose(true);
  });

  composeSubmitBtn.addEventListener('click', function () {
    submitComment();
  });

  compose.addEventListener('keydown', function (event) {
    if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') {
      event.preventDefault();
      submitComment();
    } else if (event.key === 'Escape') {
      closeCompose(true);
    }
  });

  document.addEventListener('mousedown', function (event) {
    if (event.target === addCommentBtn) {
      return;
    }
    if (!compose.hidden) {
      if (!compose.contains(event.target)) {
        clearSelectionTarget();
        closeCompose(false);
      }
      return;
    }
    // Any other click collapses the browser selection; drop the button now
    // rather than wait for selectionchange.
    if (buttonMode === 'selection') {
      buttonMode = null;
      addCommentBtn.hidden = true;
    }
  });

  function submitComment() {
    if (posting) {
      return;
    }
    var text = composeTextEl.value.trim();
    if (!text) {
      composeTextEl.focus();
      return;
    }
    posting = true;
    composeSubmitBtn.disabled = true;
    var author = composeAuthorEl.value.trim();
    postJSON('/api/comment', {
      text: text,
      type: composeType,
      line: composeLine,
      quote: composeQuote,
      author: author
    }).then(function (data) {
      var comment = data.comment;
      if (comment && comment.author) {
        composeAuthorEl.value = comment.author;
        try {
          window.localStorage.setItem(AUTHOR_KEY, comment.author);
        } catch (err) {
          // Private mode keeps the author only for this session.
        }
      }
      if (comment) {
        upsertThread(comment);
      }
      closeCompose(true);
      toast('Comment added');
    }).catch(function (err) {
      toast(err.message || 'Could not add the comment', 'error');
    }).then(function () {
      posting = false;
      composeSubmitBtn.disabled = false;
    });
  }

  // ---- connection --------------------------------------------------------

  function setConnected(connected) {
    connEl.setAttribute('data-state', connected ? 'on' : 'off');
    connLabelEl.textContent = connected ? 'Live' : 'Reconnecting…';
  }

  var wasLive = false;
  var lostToasted = false;

  function connect() {
    var source = new EventSource('/events');

    source.addEventListener('open', function () {
      wasLive = true;
      lostToasted = false;
      setConnected(true);
    });

    source.addEventListener('content', function (event) {
      var data = parseEvent(event);
      if (!data || typeof data.text !== 'string') {
        return;
      }
      state.text = data.text;
      renderContent();
    });

    source.addEventListener('threads', function (event) {
      var data = parseEvent(event);
      if (!data) {
        return;
      }
      state.threads = Array.isArray(data.threads) ? data.threads : [];
      renderOverlay();
    });

    source.onerror = function () {
      setConnected(false);
      if (wasLive && !lostToasted) {
        lostToasted = true;
        toast('Connection lost — reconnecting…', 'error');
      }
      wasLive = false;
    };
  }

  function parseEvent(event) {
    try {
      return JSON.parse(event.data);
    } catch (err) {
      return null;
    }
  }

  // The SSE stream carries the document; this one request names the file and
  // fills the page if the stream is slow to open.
  function loadMetadata() {
    fetch('/api/content').then(function (resp) {
      if (!resp.ok) {
        throw new Error('HTTP ' + resp.status);
      }
      return resp.json();
    }).then(function (data) {
      var name = String(data.path || '').split('/').pop();
      if (name) {
        fileNameEl.textContent = name;
        fileNameEl.title = data.path;
        document.title = name + ' — mdrev preview';
      }
      if (!state.text && typeof data.text === 'string') {
        state.text = data.text;
        renderContent();
      }
    }).catch(function () {
      // The filename is decoration; the stream still delivers the text.
    });
  }

  // ---- boot --------------------------------------------------------------

  var savedAuthor = '';
  try {
    savedAuthor = window.localStorage.getItem(AUTHOR_KEY) || '';
  } catch (err) {
    // Private mode keeps the author only for this session.
  }
  // Without a saved name, prefill from the server's default (git config)
  // right away instead of waiting for the first successful comment.
  if (savedAuthor) {
    composeAuthorEl.value = savedAuthor;
  } else {
    fetch('/api/author').then(function (resp) {
      return resp.ok ? resp.json() : null;
    }).then(function (data) {
      if (data && data.author && !composeAuthorEl.value) {
        composeAuthorEl.value = data.author;
      }
    }).catch(function () {
      // The server still fills the name on submit; empty field is fine.
    });
  }

  showResolvedEl.addEventListener('change', function () {
    state.showResolved = showResolvedEl.checked;
    renderOverlay();
  });

  // Height changes after the first render (mermaid, images, KaTeX) must
  // re-align the margin notes.
  if (window.ResizeObserver) {
    var documentObserver = new window.ResizeObserver(scheduleNotesLayout);
    documentObserver.observe(docEl);
    cardObserver = new window.ResizeObserver(scheduleNotesLayout);
  }
  docEl.addEventListener('load', scheduleNotesLayout, true);
  docEl.addEventListener('error', scheduleNotesLayout, true);
  notesEl.addEventListener('load', scheduleNotesLayout, true);

  window.addEventListener('resize', function () {
    scheduleNotesLayout();
    if (selectionTarget) {
      positionSelectionButton();
    }
  });

  function onBreakpointChange() {
    clearSelectionTarget();
    renderOverlay();
  }

  if (wideQuery.addEventListener) {
    wideQuery.addEventListener('change', onBreakpointChange);
  } else if (wideQuery.addListener) {
    wideQuery.addListener(onBreakpointChange);
  }

  selectType('');
  updateToolbar();
  loadMetadata();
  connect();
})();
