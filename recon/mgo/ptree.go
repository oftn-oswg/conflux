/*
   conflux - Distributed database synchronization library
	Based on the algorithm described in
		"Set Reconciliation with Nearly Optimal	Communication Complexity",
			Yaron Minsky, Ari Trachtenberg, and Richard Zippel, 2004.

   Copyright (C) 2012  Casey Marshall <casey.marshall@gmail.com>

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, version 3.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package mgo

import (
	"bytes"
	"fmt"
	. "github.com/cmars/conflux"
	. "github.com/cmars/conflux/recon"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"
	"log"
	"net"
)

type client struct {
	connect string
	session *mgo.Session
}

func NewPeer(connect string, db string) (p *Peer, err error) {
	client, err := newClient(connect)
	if err != nil {
		return nil, err
	}
	settings, err := newSettings(client, db)
	if err != nil {
		return nil, err
	}
	tree, err := newPrefixTree(settings, db)
	if err != nil {
		return nil, err
	}
	return &Peer{
		RecoverChan: make(RecoverChan),
		Settings:    settings,
		PrefixTree:  tree}, nil
}

func newClient(connect string) (c *client, err error) {
	c = &client{connect: connect}
	log.Println("Connecting to mongodb:", c.connect)
	c.session, err = mgo.Dial(c.connect)
	if err != nil {
		log.Println("Connection failed:", err)
		return
	}
	c.session.SetMode(mgo.Strong, true)
	// Conservative on writes
	c.session.EnsureSafe(&mgo.Safe{
		W:     1,
		FSync: true})
	return
}

type settings struct {
	*client
	store *mgo.Collection
	*config
}

type config struct {
	version                     string
	logName                     string
	httpPort                    int
	reconPort                   int
	partners                    []string
	filters                     []string
	threshMult                  int
	bitQuantum                  int
	mBar                        int
	splitThreshold              int
	joinThreshold               int
	numSamples                  int
	gossipIntervalSecs          int
	maxOutstandingReconRequests int
}

func newSettings(c *client, db string) (s *settings, err error) {
	s = &settings{client: c}
	s.store = c.session.DB(db).C("settings")
	return s, nil
}

func (s *settings) Init() {
	q := s.store.Find(nil)
	n, err := q.Count()
	if err != nil {
		panic(err)
	}
	if n == 0 {
		// Set defaults
		s.config = &config{
			version:                     "experimental",
			httpPort:                    11371,
			reconPort:                   11370,
			threshMult:                  DefaultThreshMult,
			bitQuantum:                  DefaultBitQuantum,
			mBar:                        DefaultMBar,
			gossipIntervalSecs:          60,
			maxOutstandingReconRequests: 100}
		// Insert object
		s.update()
	} else {
		s.config = &config{}
		err := q.One(s.config)
		if err != nil {
			panic(err)
		}
	}
	s.config.splitThreshold = s.config.threshMult * s.config.mBar
	s.config.joinThreshold = s.config.splitThreshold / 2
	s.config.numSamples = s.config.mBar + 1
}

func (s *settings) update() {
	err := s.store.Insert(s.config)
	if err != nil {
		panic(err)
	}
}

func (s *settings) Version() string {
	return s.config.version
}

func (s *settings) LogName() string {
	return s.config.logName
}

func (s *settings) HttpPort() int {
	return s.config.httpPort
}

func (s *settings) ReconPort() int {
	return s.config.reconPort
}

func (s *settings) Partners() (addrs []net.Addr) {
	for _, partner := range s.config.partners {
		addr, err := net.ResolveTCPAddr("tcp", partner)
		if err != nil {
			panic(err)
		}
		addrs = append(addrs, addr)
	}
	return
}

func (s *settings) Filters() []string {
	return s.config.filters
}

func (s *settings) ThreshMult() int {
	return s.config.threshMult
}

func (s *settings) BitQuantum() int {
	return s.config.bitQuantum
}

func (s *settings) MBar() int {
	return s.config.mBar
}

func (s *settings) SplitThreshold() int {
	return s.config.splitThreshold
}

func (s *settings) JoinThreshold() int {
	return s.config.joinThreshold
}

func (s *settings) NumSamples() int {
	return s.config.numSamples
}

func (s *settings) GossipIntervalSecs() int {
	return s.config.gossipIntervalSecs
}

func (s *settings) MaxOutstandingReconRequests() int {
	return s.config.maxOutstandingReconRequests
}

type prefixTree struct {
	*settings
	store  *mgo.Collection
	points []*Zp
}

func newPrefixTree(s *settings, db string) (tree *prefixTree, err error) {
	tree = &prefixTree{settings: s}
	tree.store = s.client.session.DB(db).C("ptree")
	err = tree.store.EnsureIndex(mgo.Index{Key: []string{"key"}})
	tree.points = Zpoints(P_SKS, tree.NumSamples())
	return tree, err
}

func (t *prefixTree) Points() []*Zp { return t.points }

func (t *prefixTree) Root() (PrefixNode, error) {
	q := t.store.Find(bson.M{"key": []byte{}})
	nd := new(nodeData)
	err := q.One(nd)
	if err != nil {
		return nil, err
	}
	return &prefixNode{prefixTree: t, nodeData: nd}, nil
}

func (t *prefixTree) Node(bs *Bitstring) (node PrefixNode, err error) {
	q := t.store.Find(bson.M{"key": bs.Bytes()})
	nd := new(nodeData)
	err = q.One(nd)
	if err != nil {
		return
	}
	return &prefixNode{prefixTree: t, nodeData: nd}, nil
}

func (t *prefixTree) Insert(z *Zp) error {
	bs := NewBitstring(P_SKS.BitLen())
	bs.SetBytes(ReverseBytes(z.Bytes()))
	root, err := t.Root()
	if err != nil {
		return err
	}
	return root.(*prefixNode).insert(z, AddElementArray(t, z), bs, 0)
}

func (t *prefixTree) Remove(z *Zp) error {
	panic("remove not impl")
}

type nodeData struct {
	key         []byte
	numElements int
	svalues     []byte
	elements    []byte
	childKeys   []int
}

type prefixNode struct {
	*prefixTree
	*nodeData
}

func (n *prefixNode) IsLeaf() bool {
	return len(n.nodeData.childKeys) == 0
}

func (n *prefixNode) Children() (result []PrefixNode) {
	key := n.Key()
	for i := 0; i < n.BitQuantum(); i++ {
		childKey := NewBitstring(key.BitLen())
		childKey.SetBytes(key.Bytes())
		for j := 0; j < n.BitQuantum(); j++ {
			if (i>>uint(j))&0x1 == 1 {
				childKey.Set(key.BitLen() + j)
			} else {
				childKey.Unset(key.BitLen() + j)
			}
		}
		child, err := n.Node(childKey)
		if err != nil {
			panic(fmt.Sprintf("Children failed on child#%v: %v", i, err))
		}
		result = append(result, child)
	}
	return
}

func (n *prefixNode) Elements() []*Zp {
	elements, err := ReadZZarray(bytes.NewBuffer(n.elements))
	if err != nil {
		panic(fmt.Sprintf("Invalid elements: %v", n.elements))
	}
	return elements
}

func (n *prefixNode) Size() int { return n.numElements }

func (n *prefixNode) SValues() []*Zp {
	svalues, err := ReadZZarray(bytes.NewBuffer(n.svalues))
	if err != nil {
		panic(fmt.Sprintf("Invalid elements: %v", n.svalues))
	}
	return svalues
}

func (n *prefixNode) Key() *Bitstring {
	key, err := ReadBitstring(bytes.NewBuffer(n.key))
	if err != nil {
		panic(fmt.Sprintf("Invalid bitstring: %v", n.key))
	}
	return key
}

func (n *prefixNode) Parent() (PrefixNode, bool) {
	if len(n.key) == 0 {
		return nil, false
	}
	key, err := ReadBitstring(bytes.NewBuffer(n.key))
	parentKey := NewBitstring(key.BitLen() - n.BitQuantum())
	parentKey.SetBytes(key.Bytes())
	parent, err := n.Node(parentKey)
	if err != nil {
		panic(fmt.Sprintf("Failed to get parent: %v", err))
	}
	return parent, true
}

func (n *prefixNode) insert(z *Zp, marray []*Zp, bs *Bitstring, depth int) (err error) {
	n.updateSvalues(z, marray)
	n.numElements++
	if n.IsLeaf() {
		if len(n.elements) > n.SplitThreshold() {
			n.split(depth)
		} else {
			var elements []*Zp
			// TODO: zzarray wrapper to perform in-place on n.elements
			elements, err = ReadZZarray(bytes.NewBuffer(n.elements))
			if err != nil {
				return
			}
			out := bytes.NewBuffer(n.elements)
			err = WriteZZarray(out, append(elements, z))
			if err == nil {
				n.elements = out.Bytes()
			}
			return
		}
	}
	child := NextChild(n, bs, depth).(*prefixNode)
	return child.insert(z, marray, bs, depth+1)
}

func (n *prefixNode) split(depth int) {
	// Create child nodes
	numChildren := 1 << uint(n.BitQuantum())
	for i := 0; i < numChildren; i++ {
		// Create new empty child node
		newChildNode(n, i)
	}
	// Move elements into child nodes
	elements, err := ReadZZarray(bytes.NewBuffer(n.elements))
	if err != nil {
		panic(fmt.Sprintf("Error reading elements: %v", err))
	}
	for _, element := range elements {
		bs := NewBitstring(P_SKS.BitLen())
		bs.SetBytes(ReverseBytes(element.Bytes()))
		child := NextChild(n, bs, depth).(*prefixNode)
		child.insert(element, AddElementArray(n.prefixTree, element), bs, depth+1)
	}
	n.elements = nil
}

func newChildNode(parent *prefixNode, childIndex int) *prefixNode {
	child := &prefixNode{nodeData: &nodeData{}, prefixTree: parent.prefixTree}
	key := parent.Key()
	childKey := NewBitstring(key.BitLen() + parent.BitQuantum())
	childKey.SetBytes(key.Bytes())
	for j := 0; j < parent.BitQuantum(); j++ {
		if (childIndex>>uint(j))&0x1 == 1 {
			childKey.Set(key.BitLen() + j)
		} else {
			childKey.Unset(key.BitLen() + j)
		}
	}
	out := bytes.NewBuffer(nil)
	err := WriteBitstring(out, childKey)
	if err != nil {
		panic(fmt.Sprintf("failed to write child key: %v", err))
	}
	child.key = out.Bytes()
	return child
}

func (n *prefixNode) updateSvalues(z *Zp, marray []*Zp) {
	if len(marray) != len(n.points) {
		panic("Inconsistent NumSamples size")
	}
	svalues, err := ReadZZarray(bytes.NewBuffer(n.svalues))
	if err != nil {
		panic(fmt.Sprintf("Failed to read svalues: %v", err))
	}
	for i := 0; i < len(marray); i++ {
		svalues[i] = Z(z.P).Mul(svalues[i], marray[i])
	}
	out := bytes.NewBuffer(nil)
	err = WriteZZarray(out, svalues)
	if err != nil {
		panic(fmt.Sprintf("Failed to read svalues: %v", err))
	}
	n.svalues = out.Bytes()
}
