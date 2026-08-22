(function(){function r(e,n,t){function o(i,f){if(!n[i]){if(!e[i]){var c="function"==typeof require&&require;if(!f&&c)return c(i,!0);if(u)return u(i,!0);var a=new Error("Cannot find module '"+i+"'");throw a.code="MODULE_NOT_FOUND",a}var p=n[i]={exports:{}};e[i][0].call(p.exports,function(r){var n=e[i][1][r];return o(n||r)},p,p.exports,r,e,n,t)}return n[i].exports}for(var u="function"==typeof require&&require,i=0;i<t.length;i++)o(t[i]);return o}return r})()({1:[function(require,module,exports){
"use strict";
const ko = require('knockout');
const octicons = require('octicons');
const components = require('ungit-components');
const navigation = require('ungit-navigation');
const programEvents = require('ungit-program-events');
const { encodePath } = require('ungit-address-parser');
components.register('header', (args) => new HeaderViewModel(args.app));
class HeaderViewModel {
    constructor(app) {
        this.app = app;
        this.theme = app.theme;
        this.showBackButton = ko.observable(false);
        this.path = ko.observable();
        this.currentVersion = ungit.version;
        this.refreshButton = components.create('refreshbutton', { isLarge: true });
        this.showAddToRepoListButton = ko.computed(() => this.path() && !this.app.repoList().includes(this.path()));
        this.addIcon = octicons.plus.toSVG({ height: 18 });
        this.backIcon = octicons['arrow-left'].toSVG({ height: 24 });
    }
    updateNode(parentElement) {
        ko.renderTemplate('header', this, {}, parentElement);
    }
    submitPath() {
        navigation.browseTo(`repository?path=${encodePath(this.path())}`);
    }
    onProgramEvent(event) {
        if (event.event == 'navigation-changed') {
            this.showBackButton(event.path != '');
            if (event.path == '')
                this.path('');
        }
        else if (event.event == 'navigated-to-path') {
            this.path(event.path);
        }
    }
    addCurrentPathToRepoList() {
        programEvents.dispatch({ event: 'request-remember-repo', repoPath: this.path() });
        return true;
    }
}

},{"knockout":undefined,"octicons":undefined,"ungit-address-parser":undefined,"ungit-components":undefined,"ungit-navigation":undefined,"ungit-program-events":undefined}]},{},[1])
//# sourceMappingURL=header.bundle.js.map
