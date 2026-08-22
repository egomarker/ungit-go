(function(){function r(e,n,t){function o(i,f){if(!n[i]){if(!e[i]){var c="function"==typeof require&&require;if(!f&&c)return c(i,!0);if(u)return u(i,!0);var a=new Error("Cannot find module '"+i+"'");throw a.code="MODULE_NOT_FOUND",a}var p=n[i]={exports:{}};e[i][0].call(p.exports,function(r){var n=e[i][1][r];return o(n||r)},p,p.exports,r,e,n,t)}return n[i].exports}for(var u="function"==typeof require&&require,i=0;i<t.length;i++)o(t[i]);return o}return r})()({1:[function(require,module,exports){
"use strict";
const ko = require('knockout');
const octicons = require('octicons');
const components = require('ungit-components');
components.register('gitErrors', (args) => new GitErrorsViewModel(args.server, args.repoPath));
class GitErrorsViewModel {
    constructor(server, repoPath) {
        this.server = server;
        this.repoPath = repoPath;
        this.gitErrors = ko.observableArray();
        this.closeIcon = octicons.x.toSVG({ height: 18 });
        this.alertIcon = octicons.alert.toSVG({ height: 24 });
    }
    updateNode(parentElement) {
        ko.renderTemplate('gitErrors', this, {}, parentElement);
    }
    onProgramEvent(event) {
        if (event.event == 'git-error')
            this._handleGitError(event);
    }
    _handleGitError(event) {
        if (event.data.repoPath != this.repoPath())
            return;
        this.gitErrors.push(new GitErrorViewModel(this, this.server, event.data));
    }
}
class GitErrorViewModel {
    constructor(gitErrors, server, data) {
        const self = this;
        this.gitErrors = gitErrors;
        this.server = server;
        this.tip = data.tip;
        this.isWarning = data.isWarning || false;
        this.command = data.command;
        this.error = data.error;
        this.stdout = data.stdout;
        this.stderr = data.stderr;
        this.showEnableBugtracking = ko.observable(false);
        this.bugReportWasSent = ungit.config.bugtracking;
        if (!data.shouldSkipReport && !ungit.config.bugtracking) {
            this.server.getPromise('/userconfig').then((userConfig) => {
                self.showEnableBugtracking(!userConfig.bugtracking);
            });
        }
    }
    dismiss() {
        this.gitErrors.gitErrors.remove(this);
    }
}

},{"knockout":undefined,"octicons":undefined,"ungit-components":undefined}]},{},[1])
//# sourceMappingURL=gitErrors.bundle.js.map
