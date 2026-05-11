/* tgOps - интерактив */

(() => {
  const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;

  // ---------- подсветка карточек за курсором ----------
  document.querySelectorAll('.card, .linkcard').forEach(el => {
    el.addEventListener('pointermove', (e) => {
      const r = el.getBoundingClientRect();
      el.style.setProperty('--mx', (e.clientX - r.left) + 'px');
      el.style.setProperty('--my', (e.clientY - r.top)  + 'px');
    });
  });

  // ---------- 3d-наклон ----------
  document.querySelectorAll('[data-tilt]').forEach(el => {
    let raf = null;
    el.addEventListener('pointermove', (e) => {
      const r = el.getBoundingClientRect();
      const px = (e.clientX - r.left) / r.width;
      const py = (e.clientY - r.top) / r.height;
      const rx = (0.5 - py) * 6;
      const ry = (px - 0.5) * 6;
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => {
        el.style.transform = `perspective(900px) rotateX(${rx}deg) rotateY(${ry}deg) translateY(-2px)`;
      });
    });
    el.addEventListener('pointerleave', () => { el.style.transform = ''; });
  });

  // ---------- магнитные кнопки ----------
  document.querySelectorAll('.btn, .nav__cta').forEach(el => {
    el.addEventListener('pointermove', (e) => {
      const r = el.getBoundingClientRect();
      const x = (e.clientX - r.left - r.width / 2) * 0.25;
      const y = (e.clientY - r.top - r.height / 2) * 0.25;
      el.style.transform = `translate(${x}px, ${y}px)`;
    });
    el.addEventListener('pointerleave', () => { el.style.transform = ''; });
  });

  // ---------- параллакс на главном экране ----------
  const floats = document.querySelectorAll('.float');
  const heroRight = document.querySelector('.hero__right');
  if (heroRight) {
    heroRight.addEventListener('pointermove', (e) => {
      const r = heroRight.getBoundingClientRect();
      const cx = (e.clientX - r.left - r.width / 2) / r.width;
      const cy = (e.clientY - r.top  - r.height / 2) / r.height;
      floats.forEach(f => {
        const d = parseFloat(f.dataset.depth || '0.05');
        f.style.transform = `translate3d(${-cx * 30 * d * 10}px, ${-cy * 30 * d * 10}px, 0)`;
      });
    });
    heroRight.addEventListener('pointerleave', () => floats.forEach(f => f.style.transform = ''));
  }

  // ---------- появление при скролле ----------
  const reveal = document.querySelectorAll('.section, .card, .linkcard, .arch, .stack, .hero__metrics li, .terminal');
  reveal.forEach(el => el.setAttribute('data-reveal', ''));
  const io = new IntersectionObserver((entries) => {
    entries.forEach(e => {
      if (e.isIntersecting) { e.target.classList.add('is-in'); io.unobserve(e.target); }
    });
  }, { threshold: 0.12, rootMargin: '0px 0px -60px 0px' });
  reveal.forEach(el => io.observe(el));

  // ---------- счётчики ----------
  const countIO = new IntersectionObserver((entries) => {
    entries.forEach(en => {
      if (!en.isIntersecting) return;
      const el = en.target;
      const target = parseInt(el.dataset.count, 10);
      const suffix = el.dataset.suffix || '';
      const dur = 1100, t0 = performance.now();
      const step = (t) => {
        const p = Math.min(1, (t - t0) / dur);
        const v = Math.round(target * (1 - Math.pow(1 - p, 3)));
        el.textContent = v + suffix;
        if (p < 1) requestAnimationFrame(step);
      };
      requestAnimationFrame(step);
      countIO.unobserve(el);
    });
  }, { threshold: 0.5 });
  document.querySelectorAll('[data-count]').forEach(el => countIO.observe(el));

  // ---------- чат ----------
  const chatBody = document.getElementById('chatBody');
  if (chatBody) {
    const script = [
      { kind: 'out', html: '<span class="mono">/status prod</span>' },
      { kind: 'typing', delay: 700 },
      { kind: 'in', html: '<b>prod-01</b> · ok ✅<br><span class="mono">cpu 38% · mem 1.4G/4G · uptime 12d</span><pre>● nginx       running\n● postgres    running\n● tgops-api   running</pre>', delay: 1100 },
      { kind: 'out', html: '<span class="mono">/deploy api v1.4.2</span>', delay: 1500 },
      { kind: 'typing', delay: 600 },
      { kind: 'in', html: '🚀 запускаю pipeline #482<div class="row"><span class="ibtn">▶ logs</span><span class="ibtn">⏸ cancel</span></div>', delay: 1100 },
      { kind: 'in', html: '✅ deploy ok · 38s<br><span class="mono">tgops-api → v1.4.2 live</span>', delay: 2200 },
      { kind: 'out', html: '<span class="mono">/alerts ack 17</span>', delay: 1800 },
      { kind: 'in', html: '👌 alert #17 acknowledged by @welizary', delay: 900 },
    ];
    let i = 0;
    const tick = async () => {
      if (i >= script.length) {
        await new Promise(r => setTimeout(r, 2400));
        chatBody.innerHTML = ''; i = 0;
      }
      const item = script[i++];
      await new Promise(r => setTimeout(r, item.delay || 600));
      if (item.kind === 'typing') {
        const t = document.createElement('div');
        t.className = 'typing';
        t.innerHTML = '<span></span><span></span><span></span>';
        chatBody.appendChild(t);
        chatBody._typing = t;
      } else {
        if (chatBody._typing) { chatBody._typing.remove(); chatBody._typing = null; }
        const m = document.createElement('div');
        m.className = 'msg msg--' + item.kind;
        m.innerHTML = item.html;
        chatBody.appendChild(m);
        while (chatBody.children.length > 5) chatBody.firstChild.remove();
      }
      requestAnimationFrame(tick);
    };
    tick();
  }

  // ---------- активная ссылка в навигации ----------
  const navLinks = document.querySelectorAll('.nav__links a');
  const sections = [...navLinks].map(a => document.querySelector(a.getAttribute('href'))).filter(Boolean);
  const navIO = new IntersectionObserver((entries) => {
    entries.forEach(e => {
      if (e.isIntersecting) {
        const id = '#' + e.target.id;
        navLinks.forEach(a => a.style.color = a.getAttribute('href') === id ? 'var(--fg)' : '');
      }
    });
  }, { rootMargin: '-40% 0px -55% 0px' });
  sections.forEach(s => navIO.observe(s));

  // ---------- прогресс скролла и липкая шапка ----------
  const progress = document.getElementById('scrollProgress');
  const nav = document.querySelector('.nav');
  const onScroll = () => {
    const h = document.documentElement;
    const pct = (h.scrollTop / (h.scrollHeight - h.clientHeight)) * 100;
    if (progress) progress.style.width = pct + '%';
    if (nav) nav.classList.toggle('is-scrolled', h.scrollTop > 20);
  };
  document.addEventListener('scroll', onScroll, { passive: true });
  onScroll();

  // ---------- бегущая строка ----------
  const marqueeTrack = document.getElementById('marqueeTrack');
  if (marqueeTrack) {
    const items = ['Go 1.22','Telegram Bot API','PostgreSQL','Docker','Docker Compose','Ansible','Terraform','GitHub Actions','Jenkins','Grafana','Loki','GCP','Nginx','Cloud Run','Webhooks'];
    const make = () => items.map(t => `<span class="marquee__item"><span class="stack__dot"></span>${t}</span>`).join('');
    marqueeTrack.innerHTML = make() + make(); // дублируем для бесшовного цикла
  }

  // ---------- печать в терминале ----------
  const term = document.getElementById('termCmd');
  if (term && !reduced) {
    const cmds = [
      'tgops status --all',
      'tgops deploy api v1.4.2 --confirm',
      'tgops logs nginx --tail 50',
      'tgops backup create db-prod',
      'tgops restart redis --graceful',
      'tgops alerts ack 17',
    ];
    let i = 0;
    const typeOne = async (txt) => {
      term.textContent = '';
      for (const ch of txt) {
        term.textContent += ch;
        await new Promise(r => setTimeout(r, 45 + Math.random() * 40));
      }
      await new Promise(r => setTimeout(r, 2200));
      // стирание
      while (term.textContent.length) {
        term.textContent = term.textContent.slice(0, -1);
        await new Promise(r => setTimeout(r, 18));
      }
      await new Promise(r => setTimeout(r, 350));
    };
    (async () => {
      while (true) {
        await typeOne(cmds[i % cmds.length]);
        i++;
      }
    })();
  }

  // ---------- смена живого статуса ----------
  const live = document.getElementById('liveStatus');
  if (live) {
    const states = [
      'все системы в норме',
      'мониторинг активен · 7 серверов',
      'pipeline #482 · running',
      'uptime · 99.97%',
      'last deploy · 38s ago',
    ];
    let i = 0;
    setInterval(() => {
      i = (i + 1) % states.length;
      live.style.opacity = '0';
      setTimeout(() => { live.textContent = states[i]; live.style.opacity = '1'; }, 280);
    }, 3400);
    live.style.transition = 'opacity .28s ease';
  }

  // ---------- скрэмбл (короткий, плавный) ----------
  document.querySelectorAll('[data-scramble]').forEach(el => {
    const original = el.textContent;
    const chars = '!<>-_/[]=+*?#';
    let raf, busy = false;
    const scramble = () => {
      if (busy) return; busy = true;
      const len = original.length;
      let frame = 0; const total = 14;
      cancelAnimationFrame(raf);
      let last = 0;
      const tick = (t) => {
        if (t - last < 55) { raf = requestAnimationFrame(tick); return; }
        last = t;
        const reveal = (frame / total) * len;
        let out = '';
        for (let i = 0; i < len; i++) {
          if (i < reveal || original[i] === ' ') out += original[i];
          else out += chars[Math.floor(Math.random() * chars.length)];
        }
        el.textContent = out;
        frame++;
        if (frame <= total) raf = requestAnimationFrame(tick);
        else { el.textContent = original; busy = false; }
      };
      raf = requestAnimationFrame(tick);
    };
    setTimeout(scramble, 500);
  });

  // ---------- переключение темы (авто + ручной выбор) ----------
  const themeBtn = document.getElementById('themeBtn');
  const applyTheme = (t) => {
    document.documentElement.setAttribute('data-theme', t);
    if (themeBtn) themeBtn.textContent = t === 'light' ? '☾' : '☀';
  };
  const stored = localStorage.getItem('tgops-theme');
  const sysDark = matchMedia('(prefers-color-scheme: dark)').matches;
  applyTheme(stored || (sysDark ? 'dark' : 'light'));
  matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (e) => {
    if (!localStorage.getItem('tgops-theme')) applyTheme(e.matches ? 'dark' : 'light');
  });
  if (themeBtn) themeBtn.addEventListener('click', () => {
    const cur = document.documentElement.getAttribute('data-theme');
    const next = cur === 'dark' ? 'light' : 'dark';
    localStorage.setItem('tgops-theme', next);
    applyTheme(next);
  });

  // ---------- переводы ru / en ----------
  // одноразовый сброс устаревшего состояния
  try {
    if (localStorage.getItem('tgops-v') !== '2') {
      localStorage.removeItem('tgops-lang');
      localStorage.removeItem('tgops-theme');
      localStorage.setItem('tgops-v', '2');
    }
  } catch(e) {}
  const I18N = {
    ru: {}, // по умолчанию текст из html
    en: {
      'nav.about':'About','nav.features':'Features','nav.stack':'Stack','nav.arch':'Architecture','nav.changelog':'Changelog','nav.contact':'Contact',
      'hero.lede':'<strong>tgOps</strong> – a Telegram bot that turns chat into a full DevOps console: server monitoring, Docker management, CI/CD deploys, backups and alerts – in one click.',
      'hero.title1':'Manage infrastructure','hero.title2':'right from Telegram',
      'btn.repo':'Source','btn.see':'What it does',
      'm.cmd':'bot commands','m.svc':'services in stack','m.ctl':'control from chat',
      'about.eyebrow':'/ about','about.title':"When the server can't wait<br/><span class=\"hero__title-grad\">and the laptop is far</span>",
      'about.lede':"Simple idea: production breaks on weekends, the laptop isn't always at hand, but Telegram is. tgOps covers a DevOps engineer's routine ops right from the messenger – check server health, restart a container, ship a release or roll back a pipeline – in seconds, from anywhere in the world.",
      'feat.eyebrow':'/ features','feat.title':'Under the hood',
      'f1.t':'Real-time monitoring','f1.d':'CPU, RAM, disk, network I/O and container health – updated right in chat with inline drill-down buttons.',
      'f2.t':'Telegram alerts','f2.d':'Any webhook lands in chat with a severity level and an <em>«Acknowledge»</em> button – critical incidents are never missed.',
      'f3.t':'Container management','f3.d':'<code>start</code>, <code>stop</code>, <code>restart</code>, <code>logs</code> for Docker – no SSH sessions, no terminal copy-paste.',
      'f4.t':'One-command deploy','f4.d':'Trigger a GitHub Actions / Jenkins pipeline from chat – with confirmation and a live build log.',
      'f5.t':'Secure access','f5.d':'Telegram-ID whitelist, per-command RBAC, audit log in PostgreSQL – who ran what and when.',
      'f6.t':'Scheduled backups','f6.d':'cron + S3-compatible storage. One-click restore with size and date preview.',
      'f7.t':'SSL / certificates','f7.d':'Reminds you 14 days before expiry and offers Certbot renewal – right from the chat.',
      'stack.eyebrow':'/ stack','stack.title':'Tech stack','stack.lede':'Standard DevOps tools wrapped into one service.',
      'arch.eyebrow':'/ architecture','arch.title':'How it works','arch.lede':'User sends a command in Telegram – bot authorizes, queues the task and streams the result back to chat.',
      'ch.eyebrow':'/ changelog','ch.title':'Latest changes','ch.lede':'Pulled live from the main branch on GitHub.',
      'ct.eyebrow':'/ contact','ct.title':'Drop a line or <span class="hero__title-grad">see the code</span>','ct.lede':"Profiles, sources, articles and CV – all in one place.",
      'footer':'© 2026 · <span class="mono">tgOps</span> · ChatOps for DevOps engineers','footer.top':'↑ top',
      'live.0':'all systems normal','live.1':'monitoring active · 7 servers','live.2':'pipeline #482 · running','live.3':'uptime · 99.97%','live.4':'last deploy · 38s ago',
    }
  };
  const langBtn = document.getElementById('langBtn');
  const setLang = (l) => {
    document.documentElement.lang = l;
    if (langBtn) langBtn.textContent = l === 'ru' ? 'RU' : 'EN';
    document.querySelectorAll('[data-i18n]').forEach(el => {
      const k = el.dataset.i18n;
      const orig = el.dataset.orig || el.innerHTML;
      if (!el.dataset.orig) el.dataset.orig = orig;
      el.innerHTML = l === 'en' ? (I18N.en[k] || orig) : orig;
    });
    localStorage.setItem('tgops-lang', l);
  };
  if (langBtn) langBtn.addEventListener('click', () => setLang(document.documentElement.lang === 'ru' ? 'en' : 'ru'));
  setLang(localStorage.getItem('tgops-lang') || 'ru');

  // ---------- список изменений из github ----------
  const chList = document.getElementById('changelogList');
  if (chList) {
    fetch('https://api.github.com/repos/WELIZARY/tgOps/commits?per_page=5')
      .then(r => r.ok ? r.json() : Promise.reject(r))
      .then(commits => {
        chList.innerHTML = commits.map(c => {
          const msg = (c.commit.message || '').split('\n')[0];
          const date = new Date(c.commit.author.date).toLocaleDateString(document.documentElement.lang === 'en' ? 'en-GB' : 'ru-RU', { day:'2-digit', month:'short', year:'numeric' });
          const sha = c.sha.slice(0,7);
          return `<li class="ch__item">
            <a href="${c.html_url}" target="_blank" rel="noopener">
              <span class="ch__sha mono">${sha}</span>
              <span class="ch__msg">${msg.replace(/[<>]/g,'')}</span>
              <span class="ch__date mono">${date}</span>
            </a>
          </li>`;
        }).join('');
      })
      .catch(() => { chList.innerHTML = '<li class="ch__item ch__item--err">не удалось загрузить · откройте репозиторий напрямую</li>'; });
  }

  // ---------- пасхалка konami ----------
  const seq = ['ArrowUp','ArrowUp','ArrowDown','ArrowDown','ArrowLeft','ArrowRight','ArrowLeft','ArrowRight','b','a'];
  let pos = 0;
  document.addEventListener('keydown', (e) => {
    pos = e.key === seq[pos] ? pos + 1 : 0;
    if (pos === seq.length) {
      pos = 0;
      document.body.classList.add('shake');
      setTimeout(() => document.body.classList.remove('shake'), 1600);
    }
  });

})();
