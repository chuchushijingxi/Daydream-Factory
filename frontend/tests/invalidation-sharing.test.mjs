import { test } from 'node:test';
import assert from 'node:assert/strict';
import { shareInvalidations } from '../src/api/invalidation-stream.ts';

test('same-account views share one subscription, transfer leadership, and reject content or foreign scope signals', async () => {
  const priorNavigator = Object.getOwnPropertyDescriptor(globalThis, 'navigator'),
    priorChannel = Object.getOwnPropertyDescriptor(globalThis, 'BroadcastChannel');
  const queues = new Map(),
    channels = new Map(),
    posted = [];
  const locks = {
    request(name, { signal }, callback) {
      return new Promise((resolve, reject) => {
        const queue = queues.get(name) || [];
        queues.set(name, queue);
        let started = false;
        const cancel = () => {
          if (started) return;
          const i = queue.indexOf(entry);
          if (i >= 0) queue.splice(i, 1);
          reject(new DOMException('Aborted', 'AbortError'));
        };
        const entry = {
          run: async () => {
            if (signal.aborted) return;
            started = true;
            signal.removeEventListener('abort', cancel);
            try {
              resolve(await callback());
            } catch (error) {
              reject(error);
            } finally {
              queue.shift();
              queue[0]?.run();
            }
          },
        };
        signal.addEventListener('abort', cancel, { once: true });
        queue.push(entry);
        if (queue.length === 1) void entry.run();
      });
    },
  };
  class Channel {
    constructor(name) {
      this.name = name;
      this.listeners = new Set();
      if (!channels.has(name)) channels.set(name, new Set());
      channels.get(name).add(this);
    }
    addEventListener(_name, listener) {
      this.listeners.add(listener);
    }
    removeEventListener(_name, listener) {
      this.listeners.delete(listener);
    }
    postMessage(data) {
      posted.push(data);
      for (const other of channels.get(this.name))
        if (other !== this) for (const listener of other.listeners) listener({ data });
    }
    close() {
      channels.get(this.name).delete(this);
    }
  }
  Object.defineProperty(globalThis, 'navigator', { value: { locks }, configurable: true });
  Object.defineProperty(globalThis, 'BroadcastChannel', { value: Channel, configurable: true });
  const controllers = Array.from({ length: 4 }, () => new AbortController()),
    events = [0, 0, 0, 0],
    readers = [],
    pending = [];
  const connect = (index) => (changed, signal) => {
    readers.push({ index, changed, signal });
    changed();
    return new Promise((resolve) => signal.addEventListener('abort', resolve, { once: true }));
  };
  const tick = () => new Promise((resolve) => setImmediate(resolve));
  try {
    pending.push(
      shareInvalidations(
        'account-a',
        'notifications',
        connect(0),
        () => events[0]++,
        controllers[0].signal,
      ),
    );
    pending.push(
      shareInvalidations(
        'account-a',
        'notifications',
        connect(1),
        () => events[1]++,
        controllers[1].signal,
      ),
    );
    pending.push(
      shareInvalidations(
        'account-b',
        'notifications',
        connect(2),
        () => events[2]++,
        controllers[2].signal,
      ),
    );
    pending.push(
      shareInvalidations(
        'account-a',
        'credits:null',
        connect(3),
        () => events[3]++,
        controllers[3].signal,
      ),
    );
    await tick();
    assert.deepEqual(
      readers.map((item) => item.index),
      [0, 2, 3],
    );
    const before = [...events];
    readers[0].changed();
    assert.equal(events[0], before[0] + 1);
    assert.equal(events[1], before[1] + 1);
    assert.equal(events[2], before[2]);
    assert.equal(events[3], before[3]);
    assert.deepEqual(posted.at(-1), { type: 'invalidate' });
    const name = [...channels.keys()].find(
      (value) => value.includes('account-a') && value.includes('notifications'),
    );
    const untouched = [...events];
    [...channels.get(name)][0].postMessage({ type: 'invalidate', body: 'forbidden' });
    assert.deepEqual(events, untouched);
    controllers[0].abort();
    await tick();
    assert.equal(readers.at(-1).index, 1);
    const after = [...events];
    readers.at(-1).changed();
    assert.equal(events[0], after[0]);
    assert.equal(events[1], after[1] + 1);
    assert.equal(events[2], after[2]);
  } finally {
    controllers.forEach((controller) => controller.abort());
    await Promise.allSettled(pending);
    if (priorNavigator) Object.defineProperty(globalThis, 'navigator', priorNavigator);
    else delete globalThis.navigator;
    if (priorChannel) Object.defineProperty(globalThis, 'BroadcastChannel', priorChannel);
    else delete globalThis.BroadcastChannel;
  }
  assert.equal(
    [...channels.values()].reduce((sum, items) => sum + items.size, 0),
    0,
  );
  assert.equal(
    [...queues.values()].reduce((sum, items) => sum + items.length, 0),
    0,
  );
});
