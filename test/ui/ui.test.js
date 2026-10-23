// Drives internal/server/ui.html in jsdom with a fake fetch. Run: make ui-test
const { test, after } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { JSDOM } = require('jsdom');

const HTML = fs.readFileSync(path.join(__dirname, '../../internal/server/ui.html'), 'utf8');
const PROVIDERS = [
  { id: 'openrouter', label: 'OpenRouter', needsKey: false, keyEnv: 'OPENROUTER_API_KEY' },
  {
    id: 'anthropic', label: 'Anthropic', needsKey: true, keyEnv: 'ANTHROPIC_API_KEY',
    keyURL: 'https://platform.claude.com/settings/keys',
    keySteps: ['Sign in to the Claude Console.', 'Click Create key.', 'Copy the sk-ant- key.'],
  },
];

// The page polls with setInterval, which would keep node alive: close every window at the end.
const windows = [];
after(() => windows.forEach((w) => w.close()));

// Fresh page per test. `models` decides the /api/models answer; `calls` records requests.
function load({ models, agents = [], providers = PROVIDERS } = {}) {
  const calls = { models: [], created: [] };
  const reply = (code, body) => ({ ok: code < 300, status: code, statusText: 'x', json: async () => body });
  const dom = new JSDOM(HTML, {
    runScripts: 'dangerously',
    pretendToBeVisual: true,
    beforeParse(w) {
      w.HTMLDialogElement.prototype.showModal = function () { this.open = true; };
      w.HTMLDialogElement.prototype.close = function () { this.open = false; };
      w.fetch = async (url, opts = {}) => {
        if (url === 'api/providers') return reply(200, providers);
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
    async setKeys(text) { $('env').value = text; fire($('env'), 'change'); await new Promise((r) => setTimeout(r, 250)); },
    async setApiKey(v) { $('apiKey').value = v; fire($('apiKey'), 'input'); await new Promise((r) => setTimeout(r, 250)); },
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

test('provider select opens on Default, then providers and Other…', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  assert.equal(ui.$('provider').value, '');
  assert.equal(ui.$('provider').selectedOptions[0].textContent, 'Default');
  assert.deepEqual(ui.optionValues('provider'), ['', 'openrouter', 'anthropic', '__custom']);
  assert.equal(ui.$('provider').closest('.row'), null);
  assert.equal(ui.$('modelSel').closest('.row'), null);
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
  assert.equal(ui.$('modelSel').disabled, false);
  assert.equal(ui.$('model').hidden, true);
  assert.equal(ui.$('modelSel').options[0].textContent, 'Default');
  assert.deepEqual(ui.optionValues('modelSel'), ['', 'a/one', 'b/two', '__custom']);
  assert.match(ui.$('modelNote').textContent, /2 models from OpenRouter/);
});

test('the selected model and provider are submitted', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  await ui.pickProvider('openrouter');
  ui.pickModel('b/two');
  ui.$('apiKey').value = 'sk-or-1';
  await ui.submit('bot');
  assert.equal(ui.calls.created.length, 1);
  assert.equal(ui.calls.created[0].model.provider, 'openrouter');
  assert.equal(ui.calls.created[0].model.name, 'b/two');
});

test('Anthropic listed from OpenRouter submits the OpenRouter provider', async () => {
  const ui = load({
    models: () => reply200({ models: ['anthropic/claude-x'] }),
    providers: [
      { id: 'openrouter', label: 'OpenRouter', needsKey: false, keyEnv: 'OPENROUTER_API_KEY' },
      {
        id: 'anthropic', label: 'Anthropic', needsKey: false,
        runtimeProvider: 'openrouter', keyEnv: 'OPENROUTER_API_KEY',
      },
    ],
  });
  await ui.open();
  await ui.pickProvider('anthropic');
  assert.equal(ui.$('keyRow').hidden, false);
  assert.equal(ui.$('modelSel').disabled, false);
  assert.equal(ui.calls.models.length, 1);
  assert.equal(ui.calls.models[0].provider, 'anthropic');
  assert.equal(ui.calls.models[0].apiKey, '');
  assert.match(ui.$('modelNote').textContent, /1 models from OpenRouter/);
  ui.pickModel('anthropic/claude-x');
  ui.$('apiKey').value = 'sk-or-1';
  ui.$('apiKey').dispatchEvent(new ui.w.Event('input', { bubbles: true }));
  await new Promise((r) => setTimeout(r, 250));
  assert.equal(ui.calls.models.length, 1, 'typing a key the listing does not need must not reload models');
  await ui.submit('bot');
  assert.equal(ui.calls.created[0].model.provider, 'openrouter');
  assert.equal(ui.calls.created[0].model.name, 'anthropic/claude-x');
  assert.equal(ui.calls.created[0].env.OPENROUTER_API_KEY, 'sk-or-1');
});

// Regression: agents were created with no provider key, so Hermes had no credentials
// and `hermes model` fell back to Anthropic OAuth (which needs the claude CLI).
test('a provider whose listing needs no key still asks for the runtime key before create', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  await ui.pickProvider('openrouter');
  assert.equal(ui.$('keyRow').hidden, false);
  ui.pickModel('a/one');
  await ui.submit('bot');
  assert.equal(ui.calls.created.length, 0);
  assert.match(ui.$('err').textContent, /OPENROUTER_API_KEY/);
  await ui.submit('bot'); // second click creates anyway, for a shared operator key
  assert.equal(ui.calls.created.length, 1);
  assert.equal(ui.calls.created[0].env.OPENROUTER_API_KEY, undefined);
});

test('a runtime key under Advanced counts as set', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  await ui.pickProvider('openrouter');
  await ui.setKeys('OPENROUTER_API_KEY=sk-or-2');
  await ui.submit('bot');
  assert.equal(ui.calls.created.length, 1);
  assert.equal(ui.calls.created[0].env.OPENROUTER_API_KEY, 'sk-or-2');
});

test('a provider that needs a key shows the key line and disables model until it is entered', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  await ui.pickProvider('anthropic');
  const after = ui.w.Node.DOCUMENT_POSITION_FOLLOWING;
  assert.ok(ui.$('provider').compareDocumentPosition(ui.$('keyRow')) & after);
  assert.ok(ui.$('keyRow').compareDocumentPosition(ui.$('modelSel')) & after);
  assert.equal(ui.$('keyRow').hidden, false);
  assert.equal(ui.$('apiKey').type, 'password');
  assert.match(ui.$('keySteps').textContent, /Click Create key/);
  assert.equal(ui.$('keyLink').getAttribute('href'), 'https://platform.claude.com/settings/keys');
  assert.equal(ui.$('modelSel').disabled, true);
  assert.equal(ui.$('modelErr').textContent, 'API key is required');
  assert.equal(ui.calls.models.length, 0);
  await ui.submit('bot');
  assert.equal(ui.calls.created.length, 0);
});

test('a partial key is not sent until typing pauses', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  await ui.pickProvider('anthropic');
  ui.$('apiKey').value = 'k';
  ui.$('apiKey').dispatchEvent(new ui.w.Event('input', { bubbles: true }));
  await new Promise((r) => setTimeout(r, 20));
  assert.equal(ui.calls.models.length, 0);
  await new Promise((r) => setTimeout(r, 230));
  assert.equal(ui.calls.models.at(-1).apiKey, 'k');
});

test('a valid key enables the model list and is saved on the agent', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  await ui.pickProvider('anthropic');
  ui.$('env').value = 'OTHER=1\nANTHROPIC_API_KEY=from-env';
  await ui.setApiKey('k');
  assert.equal(ui.$('modelSel').disabled, false);
  assert.equal(ui.$('modelErr').textContent, '');
  assert.ok(ui.optionValues('modelSel').includes('claude-x'));
  ui.pickModel('claude-x');
  await ui.submit('bot');
  assert.equal(ui.calls.created.length, 1);
  assert.equal(ui.calls.created[0].model.name, 'claude-x');
  assert.equal(ui.calls.created[0].model.provider, 'anthropic');
  assert.equal(ui.calls.created[0].env.ANTHROPIC_API_KEY, 'k');
  assert.equal(ui.calls.created[0].env.OTHER, '1');
});

test('a rejected key leaves the model disabled and shows the error', async () => {
  const ui = load({ models: byProvider });
  await ui.open();
  await ui.pickProvider('anthropic');
  await ui.setApiKey('nope');
  assert.equal(ui.$('modelSel').disabled, true);
  assert.match(ui.$('modelErr').textContent, /needs an API key/);
  assert.equal(ui.calls.created.length, 0);
});

test('a keyed provider with no steps shows a paste line and drops an unsafe link', async () => {
  const ui = load({
    models: () => reply400('down'),
    providers: [{ id: 'plain', label: 'Plain', needsKey: true, keyEnv: 'PLAIN_KEY', keyURL: 'javascript:alert(1)' }],
  });
  await ui.open();
  await ui.pickProvider('plain');
  assert.match(ui.$('keySteps').textContent, /Paste a PLAIN_KEY key/);
  assert.equal(ui.$('keyLink').hidden, true);
  assert.equal(ui.$('modelSel').disabled, true);
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
  ui.$('apiKey').value = 'sk-or-1';
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
