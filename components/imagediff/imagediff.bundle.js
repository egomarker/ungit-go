(function(){function r(e,n,t){function o(i,f){if(!n[i]){if(!e[i]){var c="function"==typeof require&&require;if(!f&&c)return c(i,!0);if(u)return u(i,!0);var a=new Error("Cannot find module '"+i+"'");throw a.code="MODULE_NOT_FOUND",a}var p=n[i]={exports:{}};e[i][0].call(p.exports,function(r){var n=e[i][1][r];return o(n||r)},p,p.exports,r,e,n,t)}return n[i].exports}for(var u="function"==typeof require&&require,i=0;i<t.length;i++)o(t[i]);return o}return r})()({1:[function(require,module,exports){
"use strict";
const ko = require('knockout');
const octicons = require('octicons');
const components = require('ungit-components');
const { encodePath } = require('ungit-address-parser');
components.register('imagediff', (args) => new ImageDiffViewModel(args));
class ImageDiffViewModel {
    constructor(args) {
        this.filename = args.filename;
        this.oldFilename = args.oldFilename;
        this.repoPath = args.repoPath;
        this.isNew = ko.observable(false);
        this.isRemoved = ko.observable(false);
        this.sha1 = args.sha1;
        this.state = ko.computed(() => {
            if (this.isNew())
                return 'new';
            if (this.isRemoved())
                return 'removed';
            return 'changed';
        });
        const gitDiffURL = `${ungit.config.rootPath}/api/diff/image?path=${encodePath(this.repoPath())}`;
        this.oldImageSrc =
            gitDiffURL + `&filename=${this.oldFilename}&version=${this.sha1 ? this.sha1 + '^' : 'HEAD'}`;
        this.newImageSrc =
            gitDiffURL + `&filename=${this.filename}&version=${this.sha1 ? this.sha1 : 'current'}`;
        this.isShowingDiffs = args.isShowingDiffs;
        this.rightArrowIcon = octicons['arrow-right'].toSVG({ height: 100 });
        this.downArrowIcon = octicons['arrow-down'].toSVG({ height: 100 });
    }
    updateNode(parentElement) {
        ko.renderTemplate('imagediff', this, {}, parentElement);
    }
    invalidateDiff() { }
    newImageError() {
        this.isRemoved(true);
    }
    oldImageError() {
        this.isNew(true);
    }
}

},{"knockout":undefined,"octicons":undefined,"ungit-address-parser":undefined,"ungit-components":undefined}]},{},[1])
//# sourceMappingURL=imagediff.bundle.js.map
