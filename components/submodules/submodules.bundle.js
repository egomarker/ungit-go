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
const programEvents = require('ungit-program-events');
const { ComponentRoot } = require('../ComponentRoot');
components.register('submodules', (args) => new SubmodulesViewModel(args.server, args.repoPath));
class SubmodulesViewModel extends ComponentRoot {
    constructor(server, repoPath) {
        super();
        this.repoPath = repoPath;
        this.server = server;
        this.fetchSubmodules = _.debounce(this._fetchSubmodules, 250, this.defaultDebounceOption);
        this.submodules = ko.observableArray();
        this.submodulesIcon = octicons['file-submodule'].toSVG({ height: 18 });
        this.closeIcon = octicons.x.toSVG({ height: 18 });
        this.linkIcon = octicons['link-external'].toSVG({ height: 18 });
    }
    onProgramEvent(event) {
        if (event.event == 'submodule-fetch') {
            this.fetchSubmodules();
        }
    }
    updateNode(parentElement) {
        this.fetchSubmodules();
        this.fetchSubmodules.flush().then((submoduleViewModel) => {
            ko.renderTemplate('submodules', submoduleViewModel, {}, parentElement);
        });
    }
    async _fetchSubmodules() {
        try {
            const submodules = await this.server.getPromise('/submodules', { path: this.repoPath() });
            this.submodules(submodules);
            return this;
        }
        catch (e) {
            ungit.logger.error('error during fetchSubmodules', e);
        }
    }
    updateSubmodules() {
        return this.server
            .postPromise('/submodules/update', { path: this.repoPath() })
            .catch((e) => this.server.unhandledRejection(e));
    }
    showAddSubmoduleDialog() {
        components.showModal('addsubmodulemodal', { path: this.repoPath() });
    }
    submoduleLinkClick(submodule) {
        window.location.href = submodule.url;
    }
    submodulePathClick(submodule) {
        window.location.href = document.URL + ungit.config.fileSeparator + submodule.path;
    }
    submoduleRemove(submodule) {
        components.showModal('yesnomodal', {
            title: 'Are you sure?',
            details: `Deleting ${submodule.name} submodule cannot be undone with Ungit-Go.`,
            closeFunc: (isYes) => {
                if (!isYes)
                    return;
                this.server
                    .delPromise('/submodules', {
                    path: this.repoPath(),
                    submodulePath: submodule.path,
                    submoduleName: submodule.name,
                })
                    .then(() => {
                    programEvents.dispatch({ event: 'submodule-fetch' });
                })
                    .catch((e) => this.server.unhandledRejection(e));
            },
        });
    }
}

},{"../ComponentRoot":1,"knockout":undefined,"lodash":undefined,"octicons":undefined,"ungit-components":undefined,"ungit-program-events":undefined}]},{},[2])
//# sourceMappingURL=submodules.bundle.js.map
