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
const moment = require('moment');
const components = require('ungit-components');
const storage = require('ungit-storage');
const { ComponentRoot } = require('../ComponentRoot');
components.register('stash', (args) => new StashViewModel(args.server, args.repoPath));
class StashItemViewModel {
    constructor(stash, data) {
        this.stash = stash;
        this.server = stash.server;
        this.id = data.reflogId;
        this.sha1 = data.sha1;
        this.title = `${data.reflogName} ${moment(new Date(data.commitDate)).fromNow()}`;
        this.message = data.message;
        this.showCommitDiff = ko.observable(false);
        this.commitDiff = ko.observable(components.create('commitDiff', {
            fileLineDiffs: data.fileLineDiffs.slice(),
            sha1: this.sha1,
            repoPath: stash.repoPath,
            server: stash.server,
            showDiffButtons: ko.observable(true),
        }));
        this.dropIcon = octicons.x.toSVG({ height: 18 });
        this.applyIcon = octicons.pencil.toSVG({ height: 20 });
    }
    apply() {
        this.server
            .delPromise(`/stashes/${this.id}`, { path: this.stash.repoPath(), apply: true })
            .catch((e) => this.server.unhandledRejection(e));
    }
    drop() {
        components.showModal('yesnomodal', {
            title: 'Are you sure you want to drop the stash?',
            details: 'This operation cannot be undone.',
            closeFunc: (isYes) => {
                if (!isYes)
                    return;
                this.server
                    .delPromise(`/stashes/${this.id}`, { path: this.stash.repoPath() })
                    .catch((e) => this.server.unhandledRejection(e));
            },
        });
    }
    toggleShowCommitDiffs() {
        this.showCommitDiff(!this.showCommitDiff());
    }
}
class StashViewModel extends ComponentRoot {
    constructor(server, repoPath) {
        super();
        this.server = server;
        this.repoPath = repoPath;
        this.refresh = _.debounce(this._refresh, 250, this.defaultDebounceOption);
        this.stashedChanges = ko.observable([]);
        this.isShow = ko.observable(storage.getItem('showStash') === 'true');
        this.visible = ko.computed(() => this.stashedChanges().length > 0 && this.isShow());
        this.expandIcon = octicons['chevron-right'].toSVG({ height: 18 });
        this.expandedIcon = octicons['chevron-down'].toSVG({ height: 22 });
        this.refresh();
    }
    updateNode(parentElement) {
        ko.renderTemplate('stash', this, {}, parentElement);
    }
    onProgramEvent(event) {
        if (event.event == 'request-app-content-refresh' || event.event == 'git-directory-changed') {
            this.refresh();
        }
    }
    async _refresh() {
        ungit.logger.debug('stash.refresh() triggered');
        try {
            const stashes = await this.server.getPromise('/stashes', { path: this.repoPath() });
            if (this.isSamePayload(stashes)) {
                return;
            }
            let changed = this.stashedChanges().length != stashes.length;
            if (!changed) {
                changed = !this.stashedChanges().every((item1) => stashes.some((item2) => item1.sha1 == item2.sha1));
            }
            if (changed) {
                this.stashedChanges(stashes.map((item) => new StashItemViewModel(this, item)));
            }
        }
        catch (err) {
            if (err.errorCode != 'no-such-path') {
                this.server.unhandledRejection(err);
            }
            else {
                ungit.logger.warn('refresh failed: ', err);
            }
        }
        finally {
            ungit.logger.debug('stash.refresh() finished');
        }
    }
    toggleShowStash() {
        this.isShow(!this.isShow());
        storage.setItem('showStash', this.isShow());
    }
}

},{"../ComponentRoot":1,"knockout":undefined,"lodash":undefined,"moment":undefined,"octicons":undefined,"ungit-components":undefined,"ungit-storage":undefined}]},{},[2])
//# sourceMappingURL=stash.bundle.js.map
