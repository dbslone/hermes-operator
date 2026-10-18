// Drives internal/server/ui.html in jsdom with a fake fetch. Run: make ui-test
const { test, after } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { JSDOM } = require('jsdom');

const HTML = fs.readFileSync(path.join(__dirname, '../../internal/server/ui.html'), 'utf8');
const PROVIDERS = [
  { id: 'openrouter', label: 'OpenRouter', needsKey: false, keyEnv: 'OPENROUTER_API_KEY' },
  { id: 'anthropic', label: 'Anthropic', needsKey: true, keyEnv: 'ANTHROPIC_API_KEY' },
];

// The page polls with setInterval, which would keep node alive: close every window at the end.
const windows = [];
after(() => windows.forEach((w) => w.close()));

// Fresh page per test. `models` decides the /api/models answer; `calls` records requests.
function load({ models, agents = [] } = {}) {
  const calls = { models: [], created: [] };
  const reply = (code, body) => ({ ok: code < 300, status: code, statusText: 'x', json: async () => body });
  const dom = new JSDOM(HTML, {
    runScripts: 'dangerously',
    pretendToBeVisual: true,
    beforeParse(w) {
      w.HTMLDialogElement.prototype.showModal = function () { this.open = true; };
      w.HTMLDialogElement.prototype.close = function () { this.open = false; };
      w.fetch = async (url, opts = {}) => {
        if (url === 'api/providers') return reply(200, PROVIDERS);
        if (url === 'api/models') {
          const body = JSON.parse(opts.body);
          calls.models.push(body);
          return models(body);
        }
        if (url === 'api/agents' && opts.method === 'POST') {
          calls.created.push(JSON.parse(opts.body));
          return reply(200, {});
        }
        if (url === 'api/agents') return reply(200, agents);
        return reply(404, { error: 'not found' });
      };
    },
  });
  const w = dom.window;
  windows.push(w);
  const $ = (id) => w.document.getElementById(id);
  const tick = () => new Promise((r) => setTimeout(r, 20));
  const fire = (el, type) => el.dispatchEvent(new w.Event(type, { bubbles: true }));
  return {
    w, $, calls,
    async open() { $('new').click(); await tick(); await tick(); },
    async pickProvider(id) { $('provider').value = id; fire($('provider'), 'change'); await tick(); },
    async setKeys(text) { $('env').value = text; fire($('env'), 'change'); await tick(); },
    pickModel(v) { $('modelSel').value = v; fire($('modelSel'), 'change'); },
    async submit(name) {
      $('name').value = name;
      $('form').dispatchEvent(new w.Event('submit', { cancelable: true, bubbles: true }));
      await tick();
    },
    optionValues: (id) => [...$(id).options].map((o) => o.value),
  };
}

const openrouter = () => reply200({ models: ['a/one', 'b/two'] });
const reply200 = (body) => ({ ok: true, status: 200, statusText: 'OK', json: async () => body });
const reply400 = (error) => ({ ok: false, status: 400, statusText: 'Bad Request', json: async () => ({ error }) });
const byProvider = (body) => body.provider === 'openrouter' ? openrouter()
  : body.apiKey === 'k' ? reply200({ models: ['claude-x'] }) : reply400('Anthropic needs an API key');

test('provider select lists default, providers and Other…', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  assert.deepEqual(ui.optionValues('provider'), ['', 'openrouter', 'anthropic', '__custom']);
});

test('without a provider the model is a plain text box', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  assert.equal(ui.$('model').hidden, false);
  assert.equal(ui.$('modelSel').hidden, true);
  assert.equal(ui.calls.models.length, 0);
});

test('choosing a provider turns model into a select of its models', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  await ui.pickProvider('openrouter');
  assert.equal(ui.$('modelSel').hidden, false);
  assert.equal(ui.$('model').hidden, true);
  assert.deepEqual(ui.optionValues('modelSel'), ['', 'a/one', 'b/two', '__custom']);
  assert.match(ui.$('modelNote').textContent, /2 models from OpenRouter/);
});

test('the selected model and provider are submitted', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  await ui.pickProvider('openrouter');
  ui.pickModel('b/two');
  await ui.submit('bot');
  assert.equal(ui.calls.created.length, 1);
  assert.equal(ui.calls.created[0].model.provider, 'openrouter');
  assert.equal(ui.calls.created[0].model.name, 'b/two');
});

test('a provider that needs a key falls back to the text box with the reason', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  await ui.pickProvider('anthropic');
  assert.equal(ui.$('modelSel').hidden, true);
  assert.equal(ui.$('model').hidden, false);
  assert.match(ui.$('modelNote').textContent, /needs an API key/);
});

test('the key typed under API keys is sent for the lookup and loads the list', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  await ui.pickProvider('anthropic');
  await ui.setKeys('ANTHROPIC_API_KEY=k');
  assert.equal(ui.calls.models.at(-1).apiKey, 'k');
  assert.equal(ui.$('modelSel').hidden, false);
  assert.ok(ui.optionValues('modelSel').includes('claude-x'));
});

test('reloading the list keeps the selected model', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  await ui.pickProvider('openrouter');
  ui.pickModel('a/one');
  await ui.setKeys('OTHER=1');
  assert.equal(ui.$('modelSel').value, 'a/one');
});

test('Other… reveals the text box and its value is submitted', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  await ui.pickProvider('openrouter');
  ui.pickModel('__custom');
  assert.equal(ui.$('model').hidden, false);
  ui.$('model').value = 'my-model';
  await ui.submit('bot2');
  assert.equal(ui.calls.created[0].model.name, 'my-model');
});

test('custom provider id is submitted and needs no model lookup', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  await ui.pickProvider('__custom');
  assert.equal(ui.$('providerCustom').hidden, false);
  ui.$('providerCustom').value = 'ollama-cloud';
  ui.$('model').value = 'llama';
  await ui.submit('bot3');
  assert.equal(ui.calls.models.length, 0);
  assert.equal(ui.calls.created[0].model.provider, 'ollama-cloud');
  assert.equal(ui.calls.created[0].model.name, 'llama');
});

test('an empty model list leaves the text box in place', async () => {
  const ui = load({ models: () => reply200({ models: [] }) });
  await ui.open();
  await ui.pickProvider('openrouter');
  assert.equal(ui.$('modelSel').hidden, true);
  assert.equal(ui.$('model').hidden, false);
});

test('providers failing to load still leaves a usable form', async () => {
  const ui = load({ models: byProvider });
  ui.w.fetch = async (url) => (url === 'api/agents' ? reply200([]) : reply400('down'));
  await ui.open();
  assert.deepEqual(ui.optionValues('provider'), ['', '__custom']);
});

test('agent cards render name, phase and model', async () => {
  const ui = load({
    models: byProvider,
    agents: [{ name: 'bot', phase: 'Ready', provider: 'openrouter', model: 'a/one', endpoint: 'http://bot:8642' }],
  });
  await new Promise((r) => setTimeout(r, 40));
  const card = ui.$('list').textContent;
  assert.match(card, /bot/);
  assert.match(card, /Ready/);
  assert.match(card, /openrouter \/ a\/one/);
});
