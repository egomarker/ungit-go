'use strict';

const assert = require('node:assert/strict');
const Module = require('node:module');
const fs = require('node:fs');
const path = require('node:path');

function observable(initialValue) {
  let value = initialValue;
  const writes = [];
  const fn = function (nextValue) {
    if (arguments.length) {
      value = nextValue;
      writes.push(nextValue);
      return fn;
    }
    return value;
  };
  fn.subscribe = () => ({ dispose() {} });
  fn._writes = writes;
  return fn;
}

function observableArray(initialValue = []) {
  const fn = observable(initialValue);
  fn.push = (value) => {
    const next = fn().slice();
    next.push(value);
    fn(next);
  };
  fn.remove = (value) => {
    fn(fn().filter((item) => item !== value));
  };
  fn.removeAll = () => fn([]);
  fn.splice = (...args) => {
    const next = fn().slice();
    const result = next.splice(...args);
    fn(next);
    return result;
  };
  fn.unshift = (value) => {
    const next = fn().slice();
    next.unshift(value);
    fn(next);
  };
  return fn;
}

function debounce(func) {
  let pending;
  function debounced(...args) {
    pending = { thisArg: this, args };
  }
  debounced.flush = () => {
    if (!pending) return undefined;
    const call = pending;
    pending = undefined;
    return func.apply(call.thisArg, call.args);
  };
  return debounced;
}

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

async function waitFor(predicate) {
  for (let i = 0; i < 50; i++) {
    if (predicate()) return;
    await new Promise((resolve) => setImmediate(resolve));
  }
  throw new Error('timed out waiting for graph load');
}

class ComponentRoot {
  constructor() {
    this._apiCache = undefined;
    this.defaultDebounceOption = {};
  }

  isSamePayload(value) {
    const serialized = JSON.stringify(value);
    if (this._apiCache === serialized) return true;
    this._apiCache = serialized;
    return false;
  }
}

const ko = {
  observable,
  observableArray,
  computed: (func) => {
    const value = () => func();
    value.subscribe = () => ({ dispose() {} });
    return value;
  },
  renderTemplate() {},
  dataFor() {},
};

const components = {
  register() {},
  create() {
    return {};
  },
};

const stubs = new Map([
  ['knockout', ko],
  ['lodash', { debounce }],
  ['moment', () => ({ valueOf: () => Date.now() })],
  ['octicons', { search: { toSVG: () => '' }, plus: { toSVG: () => '' } }],
  ['ungit-components', components],
  [
    'ungit-theme',
    {
      graphEdgeColor: () => '',
      graphAccentColor: () => '',
      graphFallbackColor: () => '',
    },
  ],
  ['./git-node', class GitNodeViewModel {}],
  ['./git-ref', class GitRefViewModel {}],
  ['./edge', class EdgeViewModel {}],
  ['../ComponentRoot', { ComponentRoot }],
]);

const originalLoad = Module._load;
Module._load = function (request, parent, isMain) {
  if (stubs.has(request)) return stubs.get(request);
  return originalLoad.call(this, request, parent, isMain);
};

global.window = { innerHeight: 0 };
global.ungit = {
  config: { numberOfNodesPerLoad: 25 },
  logger: { debug() {}, warn() {}, error() {} },
};

let GraphViewModel;
try {
  GraphViewModel = require('../components/graph/graph');
} finally {
  Module._load = originalLoad;
}

async function main() {
  const runtimeBundle = fs.readFileSync(
    path.join(__dirname, '..', 'components', 'graph', 'graph.bundle.js'),
    'utf8'
  );
  assert.match(
    runtimeBundle,
    /discarded stale generation/,
    'the embedded runtime graph bundle must contain the stale-response guard'
  );
  assert.match(
    runtimeBundle,
    /_loadNodesFromApiPending/,
    'the embedded runtime graph bundle must contain the graph-load coalescing logic'
  );

  const requests = [];
  const server = {
    getPromise(path, args) {
      assert.equal(path, '/gitlog');
      const request = deferred();
      requests.push({ args, request });
      return request.promise;
    },
    unhandledRejection(error) {
      throw error;
    },
  };

  const graph = new GraphViewModel(server, observable('/repo'));

  // Keep this test focused on request ordering rather than graph geometry.
  graph.getNode = (sha1) => ({
    sha1,
    parents: () => [],
    render() {},
    cy: () => 100,
  });
  graph.computeNode = (nodes) => nodes;
  graph.getEdge = () => ({});

  // The constructor schedules generation 1. Start it and hold the response.
  const firstDrain = graph._debouncedLoadNodesFromApi.flush();
  await waitFor(() => requests.length === 1);
  assert.equal(requests[0].args.limit, 25);

  // While generation 1 is in flight, request a larger graph. This used to be
  // able to finish first and then get overwritten by the older 25-node result.
  graph.limit(50);
  graph.loadNodesFromApi();
  graph._debouncedLoadNodesFromApi.flush();

  requests[0].request.resolve({ nodes: [{ sha1: 'stale-25' }] });
  await waitFor(() => requests.length === 2);
  assert.equal(requests[1].args.limit, 50);

  requests[1].request.resolve({ nodes: [{ sha1: 'latest-50' }] });
  await firstDrain;

  assert.deepEqual(
    graph.nodes().map((node) => node.sha1),
    ['latest-50'],
    'a stale gitlog response must never overwrite the newest graph request'
  );
  assert.equal(
    graph.nodes._writes.some((nodes) => nodes.some((node) => node.sha1 === 'stale-25')),
    false,
    'stale generations must not be rendered even temporarily once a newer request exists'
  );
  assert.equal(requests.length, 2, 'refreshes should be serialized/coalesced instead of overlapping');

  console.log('graph stale-response regression test passed');
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
