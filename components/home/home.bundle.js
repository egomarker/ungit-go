(function(){function r(e,n,t){function o(i,f){if(!n[i]){if(!e[i]){var c="function"==typeof require&&require;if(!f&&c)return c(i,!0);if(u)return u(i,!0);var a=new Error("Cannot find module '"+i+"'");throw a.code="MODULE_NOT_FOUND",a}var p=n[i]={exports:{}};e[i][0].call(p.exports,function(r){var n=e[i][1][r];return o(n||r)},p,p.exports,r,e,n,t)}return n[i].exports}for(var u="function"==typeof require&&require,i=0;i<t.length;i++)o(t[i]);return o}return r})()({1:[function(require,module,exports){
"use strict";
const ko = require('knockout');
const octicons = require('octicons');
const components = require('ungit-components');
const { encodePath } = require('ungit-address-parser');
components.register('home', (args) => new HomeViewModel(args.app));
class HomeRepositoryViewModel {
    constructor(home, path) {
        this.home = home;
        this.app = home.app;
        this.server = this.app.server;
        this.path = path;
        this.title = path;
        this.link = `${ungit.config.rootPath}/#/repository?path=${encodePath(path)}`;
        this.pathRemoved = ko.observable(false);
        this.remote = ko.observable('...');
        this.updateState();
        this.removeIcon = octicons.x.toSVG({ height: 18 });
        this.arrowIcon = octicons['arrow-right'].toSVG({ height: 24 });
    }
    updateState() {
        this.server
            .getPromise(`/fs/exists?path=${encodePath(this.path)}`)
            .then((exists) => {
            this.pathRemoved(!exists);
        })
            .catch((e) => this.server.unhandledRejection(e));
        this.server
            .getPromise(`/remotes/origin?path=${encodePath(this.path)}`)
            .then((remote) => {
            this.remote(remote.address.replace(/\/\/.*?@/, '//***@'));
        })
            .catch(() => {
            this.remote('');
        });
    }
    remove() {
        this.app.repoList.remove(this.path);
        this.home.update();
    }
}
class HomeViewModel {
    constructor(app) {
        this.app = app;
        this.repos = ko.observableArray();
        this.showNux = ko.computed(() => this.repos().length == 0);
        this.addIcon = octicons.plus.toSVG({ height: 18 });
    }
    updateNode(parentElement) {
        ko.renderTemplate('home', this, {}, parentElement);
    }
    shown() {
        this.update();
    }
    update() {
        const reposByPath = {};
        this.repos().forEach((repo) => {
            reposByPath[repo.path] = repo;
        });
        this.repos(this.app
            .repoList()
            .sort()
            .map((path) => {
            if (!reposByPath[path])
                reposByPath[path] = new HomeRepositoryViewModel(this, path);
            return reposByPath[path];
        }));
    }
    get template() {
        return 'home';
    }
}

},{"knockout":undefined,"octicons":undefined,"ungit-address-parser":undefined,"ungit-components":undefined}]},{},[1])
//# sourceMappingURL=home.bundle.js.map
