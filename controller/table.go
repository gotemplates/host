package controller

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
)

type table struct {
	egress       bool
	allowDefault bool
	mu           sync.RWMutex
	httpMatch    HttpMatcher
	uriMatch     UriMatcher
	hostCtrl     *controller
	defaultCtrl  *controller
	nilCtrl      *controller
	controllers  map[string]*controller
}

// NewEgressTable - create a new Egress table
func NewEgressTable() Table {
	return newTable(true, true)
}

// NewIngressTable - create a new Ingress table
func NewIngressTable() Table {
	return newTable(false, true)
}

func newTable(egress, allowDefault bool) *table {
	t := new(table)
	t.egress = egress
	t.allowDefault = allowDefault
	t.httpMatch = func(req *http.Request) (name string, ok bool) {
		return "", true
	}
	t.uriMatch = func(urn string, method string) (name string, ok bool) {
		return "", true
	}
	t.controllers = make(map[string]*controller, 100)
	t.hostCtrl = newDefaultController(HostControllerName)
	t.defaultCtrl = newDefaultController(DefaultControllerName)
	t.nilCtrl = newNilController(NilControllerName)
	return t
}

func (t *table) isEgress() bool { return t.egress }

func (t *table) SetHttpMatcher(fn HttpMatcher) {
	if fn == nil {
		return
	}
	t.mu.Lock()
	t.httpMatch = fn
	t.mu.Unlock()
}

func (t *table) SetUriMatcher(fn UriMatcher) {
	if fn == nil {
		return
	}
	t.mu.Lock()
	t.uriMatch = fn
	t.mu.Unlock()
}

func (t *table) SetHostController(route Route) []error {
	if t.isEgress() {
		return []error{errors.New("host controller configuration is not valid for egress traffic")}
	}
	if !t.isEgress() && (route.Retry != nil || route.Timeout != nil || route.Failover != nil) {
		return []error{errors.New("host controller configuration does not allow retry, rate limiter, or failover controllers")}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	route.Name = HostControllerName
	ctrl, errs := newController(route, t)
	if len(errs) > 0 {
		return errs
	}
	err := ctrl.validate(t.egress)
	if err != nil {
		return []error{err}
	}
	t.hostCtrl = ctrl
	return nil
}

func (t *table) SetDefaultController(route Route) []error {
	//if !t.isEgress() {
	//	return []error{errors.New("default controller configuration is not valid for ingress traffic")}
	//}
	t.mu.Lock()
	defer t.mu.Unlock()
	if route.Name == "" {
		route.Name = DefaultControllerName
	}
	act, errs := newController(route, t)
	if len(errs) > 0 {
		return errs
	}
	err := act.validate(t.egress)
	if err != nil {
		return []error{err}
	}
	t.defaultCtrl = act
	return nil
}

func (t *table) Host() Controller {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.hostCtrl
}

func (t *table) Default() Controller {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.defaultCtrl
}

func (t *table) LookupHttp(req *http.Request) Controller {
	name, ok := t.httpMatch(req)
	if !ok {
		return t.nilCtrl
	}
	if name != "" {
		if r := t.LookupByName(name); r != nil {
			return r
		}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.defaultCtrl
}

func (t *table) LookupUri(uri, method string) Controller {
	name, ok := t.uriMatch(uri, method)
	if !ok {
		return t.nilCtrl
	}
	if name != "" {
		if r := t.LookupByName(name); r != nil {
			return r
		}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.defaultCtrl
}

func (t *table) LookupByName(name string) Controller {
	if name == "" {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if r, ok := t.controllers[name]; ok {
		return r
	}
	if t.allowDefault {
		return t.defaultCtrl
	}
	return nil
}

func (t *table) AddController(route Route) []error {
	//if !t.isEgress() {
	//if route.IsConfigured() {
	//	return []error{errors.New("controller configuration can not have any controllers for ingress traffic")}
	//}
	//	route = newRoute(route.Name)
	//}
	if IsEmpty(route.Name) {
		return []error{errors.New("invalid argument: route name is empty")}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	act, errs := newController(route, t)
	if len(errs) > 0 {
		return errs
	}
	err := act.validate(t.egress)
	if err != nil {
		return []error{err}
	}
	if _, ok := t.controllers[route.Name]; ok {
		return []error{errors.New(fmt.Sprintf("invalid argument: route name is a duplicate [%v]", route.Name))}
	}
	t.controllers[route.Name] = act
	return nil
}

func (t *table) exists(name string) bool {
	if name == "" {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if _, ok := t.controllers[name]; ok {
		return true
	}
	return false
}

func (t *table) update(name string, act *controller) {
	if name == "" || act == nil {
		return
	}
	//t.mu.Lock()
	//defer t.mu.Unlock()
	delete(t.controllers, name)
	t.controllers[name] = act
	//return errors.New(fmt.Sprintf("invalid argument : controller not found [%v]", name))
}

func (t *table) count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.controllers)
}

func (t *table) isEmpty() bool {
	return t.count() == 0
}

func (t *table) remove(name string) {
	if name == "" {
		return
	}
	t.mu.Lock()
	delete(t.controllers, name)
	t.mu.Unlock()
}
