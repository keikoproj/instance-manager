package controllers

import (
	v1alpha "github.com/keikoproj/instance-manager/api/instancemgr/v1alpha1"
)

// CloudDeployer is a common interface that should be fulfilled by each provisioner
type CloudDeployer interface {
	CloudDiscovery() error            // Discover cloud resources
	StateDiscovery()                  // Derive state
	Create() error                    // CREATE Operation
	Update() error                    // UPDATE Operation
	Delete() error                    // DELETE Operation
	UpgradeNodes() error              // Process upgrade strategy
	BootstrapNodes() error            // Bootstrap Provisioned Resources
	GetState() v1alpha.ReconcileState // Gets the current state type of the instance group
	SetState(v1alpha.ReconcileState)  // Sets the current state of the instance group
	IsReady() bool                    // Returns true if state is Ready
	Locked() bool                     // Returns true if instanceGroup is locked
}

func HandleReconcileRequest(d CloudDeployer) error {
	// Cloud Discovery
	err := d.CloudDiscovery()
	if err != nil {
		return err
	}

	// State Discovery
	d.StateDiscovery()

	// CRUD Delete
	if d.GetState() == v1alpha.ReconcileInitDelete {
		err = d.Delete()
		if err != nil {
			return err
		}
	}

	// CRUD Create
	if d.GetState() == v1alpha.ReconcileInitCreate {
		err = d.Create()
		if err != nil {
			return err
		}
	}

	// CRUD Update
	if d.GetState() == v1alpha.ReconcileInitUpdate {
		err = d.Update()
		if err != nil {
			return err
		}
	}

	// CRUD Nodes Upgrade Strategy
	if d.GetState() == v1alpha.ReconcileInitUpgrade {
		// Locked
		if d.Locked() {
			d.SetState(v1alpha.ReconcileLocked)
			return nil
		}
		err = d.UpgradeNodes()
		if err != nil {
			return err
		}
	}

	// CRUD Error
	if d.GetState() == v1alpha.ReconcileErr {
		return err
	}

	// Bootstrap Nodes
	if d.IsReady() {

		err = d.BootstrapNodes()
		if err != nil {
			return err
		}

		if d.GetState() == v1alpha.ReconcileInitUpgrade {
			// Locked
			if d.Locked() {
				d.SetState(v1alpha.ReconcileLocked)
				return nil
			}
			err = d.UpgradeNodes()
			if err != nil {
				return err
			}
		}

		// Set Ready state (external end state)
		if d.GetState() == v1alpha.ReconcileModified {
			d.SetState(v1alpha.ReconcileReady)
		}
	}
	return nil
}
