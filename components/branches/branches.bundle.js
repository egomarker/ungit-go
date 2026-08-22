(function(){function r(e,n,t){function o(i,f){if(!n[i]){if(!e[i]){var c="function"==typeof require&&require;if(!f&&c)return c(i,!0);if(u)return u(i,!0);var a=new Error("Cannot find module '"+i+"'");throw a.code="MODULE_NOT_FOUND",a}var p=n[i]={exports:{}};e[i][0].call(p.exports,function(r){var n=e[i][1][r];return o(n||r)},p,p.exports,r,e,n,t)}return n[i].exports}for(var u="function"==typeof require&&require,i=0;i<t.length;i++)o(t[i]);return o}return r})()({1:[function(require,module,exports){
"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.ComponentRoot = void 0;
class ComponentRoot {
    _apiCache;
    defaultDebounceOption = {
        maxWait: 1500,
        leading: false,
        trailing: true
    };
    constructor() { }
    isSamePayload(value) {
        const jsonString = JSON.stringify(value);
        if (this._apiCache === jsonString) {
            ungit.logger.debug(`ignoring redraw for same ${this.constructor.name} payload.`);
            return true;
        }
        ungit.logger.debug(`redrawing ${this.constructor.name} payload.  \n${jsonString}`);
        this._apiCache = jsonString;
        return false;
    }
    clearApiCache() {
        this._apiCache = undefined;
    }
}
exports.ComponentRoot = ComponentRoot;

},{}],2:[function(require,module,exports){
"use strict";
const ko = require('knockout');
const _ = require('lodash');
const octicons = require('octicons');
const components = require('ungit-components');
const storage = require('ungit-storage');
const showRemote = 'showRemote';
const showBranch = 'showBranch';
const showTag = 'showTag';
const { ComponentRoot } = require('../ComponentRoot');
components.register('branches', (args) => {
    return new BranchesViewModel(args.server, args.graph, args.repoPath);
});
class BranchesViewModel extends ComponentRoot {
    constructor(server, graph, repoPath) {
        super();
        this.repoPath = repoPath;
        this.server = server;
        this.updateRefs = _.debounce(this._updateRefs, 250, this.defaultDebounceOption);
        this.branchesAndLocalTags = ko.observableArray();
        this.current = ko.observable();
        this.isShowRemote = ko.observable(storage.getItem(showRemote) != 'false');
        this.isShowBranch = ko.observable(storage.getItem(showBranch) != 'false');
        this.isShowTag = ko.observable(storage.getItem(showTag) != 'false');
        this.graph = graph;
        const setLocalStorageAndUpdate = (localStorageKey, value) => {
            storage.setItem(localStorageKey, value);
            this.updateRefs();
            return value;
        };
        this.shouldAutoFetch = ungit.config.autoFetch;
        this.isShowRemote.subscribe(() => {
            this.clearApiCache();
            setLocalStorageAndUpdate(showRemote);
        });
        this.isShowBranch.subscribe(() => {
            this.clearApiCache();
            setLocalStorageAndUpdate(showBranch);
        });
        this.isShowTag.subscribe(() => {
            this.clearApiCache();
            setLocalStorageAndUpdate(showTag);
        });
        this.refsLabel = ko.computed(() => this.current() || 'master (no commits yet)');
        this.branchIcon = octicons['git-branch'].toSVG({ height: 18 });
        this.closeIcon = octicons.x.toSVG({ height: 18 });
    }
    checkoutBranch(branch) {
        branch.checkout();
    }
    updateNode(parentElement) {
        ko.renderTemplate('branches', this, {}, parentElement);
    }
    clickFetch() {
        this.updateRefs(true);
    }
    onProgramEvent(event) {
        if (event.event === 'request-app-content-refresh' ||
            event.event === 'branch-updated' ||
            event.event === 'git-directory-changed' ||
            event.event === 'current-remote-changed') {
            this.updateRefs();
        }
    }
    async _updateRefs(forceRemoteFetch) {
        forceRemoteFetch = forceRemoteFetch || this.shouldAutoFetch || '';
        const branchesProm = this.server.getPromise('/branches', { path: this.repoPath() });
        const refsProm = this.server.getPromise('/refs', {
            path: this.repoPath(),
            remoteFetch: forceRemoteFetch,
        });
        try {
            // set current branch
            (await branchesProm).forEach((b) => {
                if (b.current) {
                    this.current(b.name);
                }
            });
        }
        catch (e) {
            this.current('~error');
            ungit.logger.warn('error while setting current branch', e);
        }
        try {
            // update branches and tags references.
            const refs = await refsProm;
            if (this.isSamePayload(refs)) {
                return;
            }
            const version = Date.now();
            const sorted = refs
                .map((r) => {
                const ref = this.graph.getRef(r.name.replace('refs/tags', 'tag: refs/tags'));
                ref.node(this.graph.getNode(r.sha1));
                ref.version = version;
                return ref;
            })
                .sort((a, b) => {
                if (a.current() || b.current()) {
                    return a.current() ? -1 : 1;
                }
                else if (a.isRemoteBranch === b.isRemoteBranch) {
                    if (a.name < b.name) {
                        return -1;
                    }
                    if (a.name > b.name) {
                        return 1;
                    }
                    return 0;
                }
                else {
                    return a.isRemoteBranch ? 1 : -1;
                }
            })
                .filter((ref) => {
                if (ref.localRefName == 'refs/stash')
                    return false;
                if (ref.localRefName.endsWith('/HEAD'))
                    return false;
                if (!this.isShowRemote() && ref.isRemote)
                    return false;
                if (!this.isShowBranch() && ref.isBranch)
                    return false;
                if (!this.isShowTag() && ref.isTag)
                    return false;
                return true;
            });
            this.branchesAndLocalTags(sorted);
            this.graph.refs().forEach((ref) => {
                // ref was removed from another source
                if (!ref.isRemoteTag && ref.value !== 'HEAD' && (!ref.version || ref.version < version)) {
                    ref.remove(true);
                }
            });
        }
        catch (e) {
            ungit.logger.error('error during branch update: ', e);
        }
    }
    branchRemove(branch) {
        let details = `"${branch.refName}"`;
        if (branch.isRemoteBranch) {
            details = `<code style='font-size: 100%'>REMOTE</code> ${details}`;
        }
        components.showModal('yesnomodal', {
            title: 'Are you sure?',
            details: 'Deleting ' + details + ' branch cannot be undone with Ungit-Go.',
            closeFunc: (isYes) => {
                if (!isYes)
                    return;
                return branch.remove();
            },
        });
    }
}

},{"../ComponentRoot":1,"knockout":undefined,"lodash":undefined,"octicons":undefined,"ungit-components":undefined,"ungit-storage":undefined}]},{},[2])
//# sourceMappingURL=branches.bundle.js.map
